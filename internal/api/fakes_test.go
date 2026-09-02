package api_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/user"
	"github.com/garamsh/garam/internal/watch"
)

// fakeProvider proves the provider abstraction: a second provider is a
// value satisfying authn.Provider, and call sites never change.
type fakeProvider struct {
	name     string
	identity *authn.Identity
	err      error
}

func (p *fakeProvider) Name() string { return p.name }

func (p *fakeProvider) AuthCodeURL(state, verifier string) string {
	return "https://idp.example/auth?state=" + state
}

func (p *fakeProvider) Exchange(_ context.Context, _, _ string) (*authn.Identity, error) {
	return p.identity, p.err
}

// fakeUserStore is an in-memory user.Store double. Unimplemented
// methods panic via the embedded interface; tests only touch what is
// implemented.
type fakeUserStore struct {
	user.Store
	mu   sync.Mutex
	byID map[string]*user.User
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{byID: map[string]*user.User{}}
}

// Create records a user. A second row under one GRN is refused the way
// the postgres store refuses it — the driver's own error through
// pgxerr.Classify (internal/user/postgres.go) — because a double this
// surface reads its own answers from must carry what production
// carries, text included.
func (s *fakeUserStore) Create(_ context.Context, u *user.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, held := s.byID[u.GRN().String()]; held {
		return pgxerr.Classify(&pgconn.PgError{
			Severity:       "ERROR",
			Code:           "23505",
			Message:        `duplicate key value violates unique constraint "users_pkey"`,
			ConstraintName: "users_pkey",
			TableName:      "users",
		}, "create", nil, nil)
	}
	s.byID[u.GRN().String()] = u
	return nil
}

func (s *fakeUserStore) Get(_ context.Context, g grn.GRN) (*user.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[g.String()]
	if !ok {
		return nil, user.ErrUserNotFound
	}
	return u, nil
}

func (s *fakeUserStore) ListByGRNs(_ context.Context, grns []grn.GRN) ([]*user.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]*user.User, 0, len(grns))
	for _, g := range grns {
		if u, ok := s.byID[g.String()]; ok {
			users = append(users, u)
		}
	}
	return users, nil
}

// remove drops a user row, leaving memberships that name it
// unresolvable.
func (s *fakeUserStore) remove(g grn.GRN) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, g.String())
}

// grantRoles replaces the stored user with one carrying the given
// direct roles; login creates users without any.
func (s *fakeUserStore) grantRoles(g grn.GRN, roles ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[g.String()] = user.New(g, s.byID[g.String()].Nickname(), roles...)
}

// fakeSessionStore is an in-memory authn.SessionStore double.
type fakeSessionStore struct {
	mu   sync.Mutex
	byID map[string]*authn.Session
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{byID: map[string]*authn.Session{}}
}

func (s *fakeSessionStore) Create(_ context.Context, token string, sess *authn.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[token] = sess
	return nil
}

func (s *fakeSessionStore) Get(_ context.Context, token string) (*authn.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byID[token]
	if !ok {
		return nil, authn.ErrSessionNotFound
	}
	return sess, nil
}

func (s *fakeSessionStore) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, token)
	return nil
}

// fakeOAuthStateStore is an in-memory authn.OAuthStateStore double. It
// holds each live state's expiry, as the table holds a row's.
type fakeOAuthStateStore struct {
	mu       sync.Mutex
	expiryOf map[string]time.Time
}

func newFakeOAuthStateStore() *fakeOAuthStateStore {
	return &fakeOAuthStateStore{expiryOf: map[string]time.Time{}}
}

func (s *fakeOAuthStateStore) Issue(_ context.Context, state string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	// The store the double stands in for prunes on every issue and
	// refuses past MaxLiveOAuthStates live states; the double decides
	// the same way, so a test meets the bound the real store enforces.
	for held, expiry := range s.expiryOf {
		if !now.Before(expiry) {
			delete(s.expiryOf, held)
		}
	}
	if len(s.expiryOf) >= authn.MaxLiveOAuthStates {
		return authn.ErrOAuthStateRateLimited
	}
	s.expiryOf[state] = now.Add(ttl)
	return nil
}

func (s *fakeOAuthStateStore) Consume(_ context.Context, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.expiryOf[state]
	if !ok || !time.Now().UTC().Before(expiry) {
		return authn.ErrOAuthStateNotLive
	}
	delete(s.expiryOf, state)
	return nil
}

// fakeOrgStore is an in-memory org.Store double for the org, group,
// project, membership, and invitation methods the API exercises.
type fakeOrgStore struct {
	org.Store
	mu             sync.Mutex
	orgs           map[string]*org.Organization
	groups         map[string]*org.Group
	groupMembers   map[string][]grn.GRN
	projects       map[string]*org.Project
	projectMembers map[string][]grn.GRN
	members        map[string]*org.Membership
	invitations    map[string]*org.Invitation
	accepted       map[string]bool
}

func newFakeOrgStore() *fakeOrgStore {
	return &fakeOrgStore{
		orgs:           map[string]*org.Organization{},
		groups:         map[string]*org.Group{},
		groupMembers:   map[string][]grn.GRN{},
		projects:       map[string]*org.Project{},
		projectMembers: map[string][]grn.GRN{},
		members:        map[string]*org.Membership{},
		invitations:    map[string]*org.Invitation{},
		accepted:       map[string]bool{},
	}
}

func memberKey(orgGRN, userGRN grn.GRN) string { return orgGRN.String() + "|" + userGRN.String() }

func (s *fakeOrgStore) CreateOrg(_ context.Context, o *org.Organization) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgs[o.GRN().String()] = o
	return nil
}

func (s *fakeOrgStore) GetOrg(_ context.Context, g grn.GRN) (*org.Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orgs[g.String()]
	if !ok {
		return nil, org.ErrOrgNotFound
	}
	return o, nil
}

func (s *fakeOrgStore) UpdateOrg(_ context.Context, g grn.GRN, p org.Patch) (*org.Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orgs[g.String()]
	if !ok {
		return nil, org.ErrOrgNotFound
	}
	name := o.Name()
	if p.Name != nil {
		name = *p.Name
	}
	updated := org.NewOrganization(g, name)
	s.orgs[g.String()] = updated
	return updated, nil
}

func (s *fakeOrgStore) DeleteOrg(_ context.Context, g grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[g.String()]; !ok {
		return org.ErrOrgNotFound
	}
	delete(s.orgs, g.String())
	return nil
}

func (s *fakeOrgStore) ListOrgsByMember(_ context.Context, userGRN grn.GRN, _ listfilter.Filter) ([]*org.Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*org.Organization{}
	for _, m := range s.members {
		if m.User() == userGRN {
			out = append(out, s.orgs[m.Organization().String()])
		}
	}
	return out, nil
}

func (s *fakeOrgStore) CreateGroup(_ context.Context, g *org.Group) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups[g.GRN().String()] = g
	return nil
}

func (s *fakeOrgStore) GetGroup(_ context.Context, g grn.GRN) (*org.Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gr, ok := s.groups[g.String()]
	if !ok {
		return nil, org.ErrGroupNotFound
	}
	return gr, nil
}

func (s *fakeOrgStore) ListGroups(_ context.Context, orgGRN grn.GRN, _ listfilter.Filter) ([]*org.Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*org.Group{}
	for _, gr := range s.groups {
		if gr.Organization() == orgGRN {
			out = append(out, gr)
		}
	}
	slices.SortFunc(out, func(a, b *org.Group) int {
		return strings.Compare(a.GRN().String(), b.GRN().String())
	})
	return out, nil
}

func (s *fakeOrgStore) UpdateGroup(_ context.Context, g grn.GRN, p org.GroupPatch) (*org.Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gr, ok := s.groups[g.String()]
	if !ok {
		return nil, org.ErrGroupNotFound
	}
	name := gr.Name()
	if p.Name != nil {
		name = *p.Name
	}
	roles := gr.Roles()
	if p.Roles != nil {
		roles = p.Roles
	}
	updated := org.NewGroup(g, name, gr.Organization(), roles...)
	s.groups[g.String()] = updated
	return updated, nil
}

func (s *fakeOrgStore) DeleteGroup(_ context.Context, g grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.groups[g.String()]; !ok {
		return org.ErrGroupNotFound
	}
	delete(s.groups, g.String())
	delete(s.groupMembers, g.String())
	return nil
}

func (s *fakeOrgStore) ListGroupsByMember(_ context.Context, orgGRN, memberGRN grn.GRN) ([]grn.GRN, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []grn.GRN{}
	for key, members := range s.groupMembers {
		gr, ok := s.groups[key]
		if !ok || gr.Organization() != orgGRN || !slices.Contains(members, memberGRN) {
			continue
		}
		out = append(out, gr.GRN())
	}
	slices.SortFunc(out, func(a, b grn.GRN) int { return strings.Compare(a.String(), b.String()) })
	return out, nil
}

func (s *fakeOrgStore) AddGroupMember(_ context.Context, groupGRN, memberGRN grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.groups[groupGRN.String()]; !ok {
		return org.ErrGroupNotFound
	}
	if slices.Contains(s.groupMembers[groupGRN.String()], memberGRN) {
		return errorx.New(errorx.KindAlreadyExists, "group member already exists")
	}
	s.groupMembers[groupGRN.String()] = append(s.groupMembers[groupGRN.String()], memberGRN)
	return nil
}

func (s *fakeOrgStore) RemoveGroupMember(_ context.Context, groupGRN, memberGRN grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := groupGRN.String()
	// A Group that does not exist holds no members either, so it takes
	// the same answer here as it does from the store.
	if !slices.Contains(s.groupMembers[k], memberGRN) {
		return org.ErrGroupMemberNotFound
	}
	s.groupMembers[k] = slices.DeleteFunc(s.groupMembers[k], func(m grn.GRN) bool { return m == memberGRN })
	return nil
}

func (s *fakeOrgStore) ListGroupMembers(_ context.Context, groupGRN grn.GRN) ([]grn.GRN, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.groupMembers[groupGRN.String()]), nil
}

func (s *fakeOrgStore) CreateProject(_ context.Context, p *org.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[p.GRN().String()] = p
	return nil
}

func (s *fakeOrgStore) GetProject(_ context.Context, g grn.GRN) (*org.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[g.String()]
	if !ok {
		return nil, org.ErrProjectNotFound
	}
	return p, nil
}

func (s *fakeOrgStore) ListProjects(_ context.Context, orgGRN grn.GRN, _ listfilter.Filter) ([]*org.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*org.Project{}
	for _, p := range s.projects {
		if p.Organization() == orgGRN {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b *org.Project) int {
		return strings.Compare(a.GRN().String(), b.GRN().String())
	})
	return out, nil
}

func (s *fakeOrgStore) UpdateProject(_ context.Context, g grn.GRN, patch org.ProjectPatch) (*org.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[g.String()]
	if !ok {
		return nil, org.ErrProjectNotFound
	}
	name := p.Name()
	if patch.Name != nil {
		name = *patch.Name
	}
	roles := p.Roles()
	if patch.Roles != nil {
		roles = patch.Roles
	}
	updated := org.NewProject(g, name, p.Organization(), roles...)
	s.projects[g.String()] = updated
	return updated, nil
}

func (s *fakeOrgStore) DeleteProject(_ context.Context, g grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[g.String()]; !ok {
		return org.ErrProjectNotFound
	}
	delete(s.projects, g.String())
	delete(s.projectMembers, g.String())
	return nil
}

func (s *fakeOrgStore) AddProjectMember(_ context.Context, projectGRN, memberGRN grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[projectGRN.String()]; !ok {
		return org.ErrProjectNotFound
	}
	if slices.Contains(s.projectMembers[projectGRN.String()], memberGRN) {
		return errorx.New(errorx.KindAlreadyExists, "project member already exists")
	}
	s.projectMembers[projectGRN.String()] = append(s.projectMembers[projectGRN.String()], memberGRN)
	return nil
}

func (s *fakeOrgStore) RemoveProjectMember(_ context.Context, projectGRN, memberGRN grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := projectGRN.String()
	if !slices.Contains(s.projectMembers[k], memberGRN) {
		return org.ErrProjectMemberNotFound
	}
	s.projectMembers[k] = slices.DeleteFunc(s.projectMembers[k], func(m grn.GRN) bool { return m == memberGRN })
	return nil
}

func (s *fakeOrgStore) ListProjectMembers(_ context.Context, projectGRN grn.GRN) ([]grn.GRN, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.projectMembers[projectGRN.String()]), nil
}

func (s *fakeOrgStore) AddMember(_ context.Context, m *org.Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memberKey(m.Organization(), m.User())
	if _, ok := s.members[k]; ok {
		return org.ErrMemberAlreadyExists
	}
	s.members[k] = m
	return nil
}

func (s *fakeOrgStore) GetMember(_ context.Context, orgGRN, userGRN grn.GRN) (*org.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[memberKey(orgGRN, userGRN)]
	if !ok {
		return nil, org.ErrMemberNotFound
	}
	return m, nil
}

func (s *fakeOrgStore) ListMembers(_ context.Context, orgGRN grn.GRN) ([]*org.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*org.Membership{}
	for _, m := range s.members {
		if m.Organization() == orgGRN {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *fakeOrgStore) CreateInvitation(_ context.Context, inv *org.Invitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invitations[inv.Token()] = inv
	return nil
}

func (s *fakeOrgStore) GetInvitation(_ context.Context, token string) (*org.Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invitations[token]
	if !ok {
		return nil, org.ErrInvitationNotFound
	}
	return inv, nil
}

func (s *fakeOrgStore) AcceptInvitation(_ context.Context, token string, _ grn.GRN) (*org.Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invitations[token]
	if !ok {
		return nil, org.ErrInvitationNotFound
	}
	if s.accepted[token] {
		return nil, org.ErrInvitationAccepted
	}
	s.accepted[token] = true
	return inv, nil
}

// fakeAgentStore is an in-memory agent.Store double. The API reads
// agents and the assignments holding them and writes definitions; the
// write methods an operator reaches over the machine listener stay
// unimplemented and panic through the embedded interface.
//
// Definitions are held apart from agents, as the schema holds them:
// defining an agent puts nothing in `agents`, so a test that finds one
// there found what an operator reported.
type fakeAgentStore struct {
	agent.Store
	mu          sync.Mutex
	agents      map[string]*agent.Agent
	assignments map[string]*agent.Assignment
	definitions map[string]*agent.Definition
}

func newFakeAgentStore() *fakeAgentStore {
	return &fakeAgentStore{
		agents:      map[string]*agent.Agent{},
		assignments: map[string]*agent.Assignment{},
		definitions: map[string]*agent.Definition{},
	}
}

func (s *fakeAgentStore) Create(_ context.Context, a *agent.Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[a.GRN().String()] = a
	return nil
}

func (s *fakeAgentStore) Get(_ context.Context, g grn.GRN) (*agent.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[g.String()]
	if !ok {
		return nil, agent.ErrAgentNotFound
	}
	return a, nil
}

func (s *fakeAgentStore) ListByOrg(_ context.Context, orgGRN grn.GRN, _ listfilter.Filter) ([]*agent.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*agent.Agent{}
	for _, a := range s.agents {
		if a.Organization() == orgGRN {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b *agent.Agent) int {
		return strings.Compare(a.GRN().String(), b.GRN().String())
	})
	return out, nil
}

func (s *fakeAgentStore) Assign(_ context.Context, agentGRN, operatorGRN grn.GRN) (*agent.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agents[agentGRN.String()]; !ok {
		return nil, agent.ErrAgentNotFound
	}
	epoch := 1
	if held, ok := s.assignments[agentGRN.String()]; ok {
		epoch = held.Epoch() + 1
	}
	a := agent.NewAssignment(agentGRN, operatorGRN, epoch)
	s.assignments[agentGRN.String()] = a
	return a, nil
}

func (s *fakeAgentStore) GetAssignment(_ context.Context, agentGRN grn.GRN) (*agent.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assignments[agentGRN.String()]
	if !ok {
		return nil, agent.ErrAssignmentNotFound
	}
	return a, nil
}

func (s *fakeAgentStore) Define(_ context.Context, d *agent.Definition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.definitions[d.Agent().String()] = d
	return nil
}

func (s *fakeAgentStore) ListDefinitionsByOrg(_ context.Context, orgGRN grn.GRN) ([]*agent.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*agent.Definition{}
	for _, d := range s.definitions {
		if d.Organization() == orgGRN {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b *agent.Definition) int {
		return strings.Compare(a.Agent().String(), b.Agent().String())
	})
	return out, nil
}

func (s *fakeAgentStore) ListAssignmentsByOrg(_ context.Context, orgGRN grn.GRN) ([]*agent.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*agent.Assignment{}
	for key, a := range s.assignments {
		if held, ok := s.agents[key]; ok && held.Organization() == orgGRN {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b *agent.Assignment) int {
		return strings.Compare(a.Agent().String(), b.Agent().String())
	})
	return out, nil
}

// fakeConsentStore is an in-memory message.ConsentStore double. It
// decides the way the real store does: one row per ordered pair, each
// half standing from its side's grant until that side withdraws, and a
// row neither half stands in is gone.
type fakeConsentStore struct {
	message.ConsentStore
	mu     sync.Mutex
	grants map[string]map[string]bool // pair key → org GRN → half stands
	err    error
}

func newFakeConsentStore() *fakeConsentStore {
	return &fakeConsentStore{grants: map[string]map[string]bool{}}
}

func consentPairKey(a, b grn.GRN) string {
	if b.String() < a.String() {
		a, b = b, a
	}
	return a.String() + "|" + b.String()
}

func (s *fakeConsentStore) Grant(_ context.Context, orgGRN, other grn.GRN) (*message.Consent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := consentPairKey(orgGRN, other)
	halves, ok := s.grants[k]
	if !ok {
		halves = map[string]bool{}
		s.grants[k] = halves
	}
	halves[orgGRN.String()] = true
	a, b := orgGRN, other
	if b.String() < a.String() {
		a, b = b, a
	}
	var aAt, bAt *time.Time
	if halves[a.String()] {
		aAt = ptrTo(time.Now())
	}
	if halves[b.String()] {
		bAt = ptrTo(time.Now())
	}
	return message.NewConsent(a, b, aAt, bAt), nil
}

func (s *fakeConsentStore) Withdraw(_ context.Context, orgGRN, other grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := consentPairKey(orgGRN, other)
	halves, ok := s.grants[k]
	if !ok {
		return nil
	}
	delete(halves, orgGRN.String())
	if len(halves) == 0 {
		delete(s.grants, k)
	}
	return nil
}

func (s *fakeConsentStore) Stands(_ context.Context, a, b grn.GRN) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	halves, ok := s.grants[consentPairKey(a, b)]
	return ok && halves[a.String()] && halves[b.String()], nil
}

func (s *fakeConsentStore) List(_ context.Context, orgGRN grn.GRN) ([]*message.Consent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*message.Consent{}
	for k, halves := range s.grants {
		parts := strings.SplitN(k, "|", 2)
		a, errA := grn.Parse(parts[0])
		b, errB := grn.Parse(parts[1])
		if errA != nil || errB != nil {
			continue
		}
		if a != orgGRN && b != orgGRN {
			continue
		}
		var aAt, bAt *time.Time
		if halves[a.String()] {
			aAt = ptrTo(time.Now())
		}
		if halves[b.String()] {
			bAt = ptrTo(time.Now())
		}
		out = append(out, message.NewConsent(a, b, aAt, bAt))
	}
	slices.SortFunc(out, func(x, y *message.Consent) int {
		return strings.Compare(x.OrgA().String()+x.OrgB().String(), y.OrgA().String()+y.OrgB().String())
	})
	return out, nil
}

// ptrTo returns a pointer to v.
func ptrTo[T any](v T) *T { return &v }

// fakeWatchLeases is a watch.Store in memory: one entry per open watch,
// which is what says whether a watch outlived the stream it belonged to.
type fakeWatchLeases struct {
	mu           sync.Mutex
	byID         map[watch.WatchID]grn.GRN
	closeRefused bool
}

func newFakeWatchLeases() *fakeWatchLeases {
	return &fakeWatchLeases{byID: map[watch.WatchID]grn.GRN{}}
}

func (l *fakeWatchLeases) Open(_ context.Context, id watch.WatchID, agent grn.GRN, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byID[id] = agent
	return nil
}

func (l *fakeWatchLeases) Refresh(_ context.Context, id watch.WatchID, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, live := l.byID[id]; !live {
		return watch.ErrWatchLapsed
	}
	return nil
}

func (l *fakeWatchLeases) Close(_ context.Context, id watch.WatchID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closeRefused {
		return errorx.New(errorx.KindUnavailable, "watch lease store is gone")
	}
	delete(l.byID, id)
	return nil
}

// refuseClose stands in for a store that held the watch open and is
// gone by the time the stream ends and the lease is dropped.
func (l *fakeWatchLeases) refuseClose() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeRefused = true
}

func (l *fakeWatchLeases) Count(_ context.Context, agent grn.GRN) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, watched := range l.byID {
		if watched == agent {
			count++
		}
	}
	return count, nil
}

// held is how many watches are open in total.
func (l *fakeWatchLeases) held() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.byID)
}

// fakeTurnEvents is the watch.Relay a forwarded event arrives over. The
// process that received it from an adapter is another one (ADR-0017), so
// announcing here is what that process does and the receiver registered
// below is this one.
type fakeTurnEvents struct {
	mu      sync.Mutex
	deliver func(grn.GRN, watch.Event)
	ready   chan struct{}
	once    sync.Once
}

func newFakeTurnEvents() *fakeTurnEvents {
	return &fakeTurnEvents{ready: make(chan struct{})}
}

func (r *fakeTurnEvents) Announce(_ context.Context, agent grn.GRN, e watch.Event) error {
	r.mu.Lock()
	deliver := r.deliver
	r.mu.Unlock()
	if deliver != nil {
		deliver(agent, e)
	}
	return nil
}

func (r *fakeTurnEvents) Receive(ctx context.Context, deliver func(grn.GRN, watch.Event)) error {
	r.mu.Lock()
	r.deliver = deliver
	r.mu.Unlock()
	r.once.Do(func() { close(r.ready) })
	<-ctx.Done()
	return nil
}

// fakePasswordCredentialStore is an in-memory
// authn.PasswordCredentialStore double.
type fakePasswordCredentialStore struct {
	mu             sync.Mutex
	byUser         map[string]authn.Verifier
	replaceRefused bool
}

func newFakePasswordCredentialStore() *fakePasswordCredentialStore {
	return &fakePasswordCredentialStore{byUser: map[string]authn.Verifier{}}
}

// Create records the first verifier for a user. A second one is a
// conflict, as the table's primary key makes it.
func (s *fakePasswordCredentialStore) Create(_ context.Context, userGRN grn.GRN, v authn.Verifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, held := s.byUser[userGRN.String()]; held {
		return errorx.New(errorx.KindAlreadyExists, "password credential exists")
	}
	s.byUser[userGRN.String()] = v
	return nil
}

func (s *fakePasswordCredentialStore) Get(_ context.Context, userGRN grn.GRN) (authn.Verifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, held := s.byUser[userGRN.String()]
	if !held {
		return "", authn.ErrPasswordCredentialNotFound
	}
	return v, nil
}

func (s *fakePasswordCredentialStore) Replace(_ context.Context, userGRN grn.GRN, v authn.Verifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replaceRefused {
		return errorx.New(errorx.KindUnavailable, "password credential store is gone")
	}
	s.byUser[userGRN.String()] = v
	return nil
}

// refuseReplace stands in for a store that answered the read a sign-in
// needed and is gone by the write that follows it.
func (s *fakePasswordCredentialStore) refuseReplace() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceRefused = true
}

// held returns the verifier stored for a user, so a test can read what
// a sign-in left behind.
func (s *fakePasswordCredentialStore) held(userGRN grn.GRN) authn.Verifier {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byUser[userGRN.String()]
}

// fakePasswordAttemptStore is an in-memory
// authn.PasswordAttemptStore double. It holds each live attempt's
// expiry, as the table holds a row's.
type fakePasswordAttemptStore struct {
	mu             sync.Mutex
	expiryOf       map[string]time.Time
	releaseRefused bool
}

func newFakePasswordAttemptStore() *fakePasswordAttemptStore {
	return &fakePasswordAttemptStore{expiryOf: map[string]time.Time{}}
}

// Record decides as the statement it stands in for decides: it prunes
// what lapsed and writes nothing past MaxLivePasswordAttempts, so a
// test meets the bound the real store enforces.
func (s *fakePasswordAttemptStore) Record(_ context.Context, attempt string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for held, expiry := range s.expiryOf {
		if !now.Before(expiry) {
			delete(s.expiryOf, held)
		}
	}
	if len(s.expiryOf) >= authn.MaxLivePasswordAttempts {
		return authn.ErrPasswordAttemptRateLimited
	}
	s.expiryOf[attempt] = now.Add(ttl)
	return nil
}

func (s *fakePasswordAttemptStore) Release(_ context.Context, attempt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.releaseRefused {
		return errorx.New(errorx.KindUnavailable, "password attempt store is gone")
	}
	delete(s.expiryOf, attempt)
	return nil
}

// refuseRelease stands in for a store that recorded the attempt and is
// gone by the time the verification that succeeded frees it.
func (s *fakePasswordAttemptStore) refuseRelease() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseRefused = true
}

// fill records n attempts, standing in for the verifications a caller
// already drove. Filling the store directly rather than through the
// endpoint keeps the test off n Argon2id computations, which are the
// cost the bound exists to refuse.
func (s *fakePasswordAttemptStore) fill(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry := time.Now().UTC().Add(time.Hour)
	for i := range n {
		s.expiryOf[fmt.Sprintf("filled-%d", i)] = expiry
	}
}

// releaseOne frees a single slot, standing in for a verification that
// succeeded.
func (s *fakePasswordAttemptStore) releaseOne() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for held := range s.expiryOf {
		delete(s.expiryOf, held)
		return
	}
}

type fakeOperatorStore struct {
	operator.Store
	mu        sync.Mutex
	operators map[string]*operator.Operator
}

func newFakeOperatorStore() *fakeOperatorStore {
	return &fakeOperatorStore{operators: map[string]*operator.Operator{}}
}

func (s *fakeOperatorStore) Register(_ context.Context, o *operator.Operator) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.operators[o.GRN().String()]; ok {
		return operator.ErrOperatorAlreadyRegistered
	}
	s.operators[o.GRN().String()] = o
	return nil
}

func (s *fakeOperatorStore) Get(_ context.Context, g grn.GRN) (*operator.Operator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.operators[g.String()]
	if !ok {
		return nil, operator.ErrOperatorNotFound
	}
	return o, nil
}

func (s *fakeOperatorStore) ListByOrg(_ context.Context, orgGRN grn.GRN) ([]*operator.Operator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*operator.Operator{}
	for _, o := range s.operators {
		if o.Organization() == orgGRN {
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b *operator.Operator) int {
		return strings.Compare(a.GRN().String(), b.GRN().String())
	})
	return out, nil
}
