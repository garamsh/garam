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
// pgxerr.Classify (internal/user/postgres/postgres.go) — because a
// double this surface reads its own answers from must carry what
// production carries, text included.
func (s *fakeUserStore) Create(_ context.Context, u *user.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, held := s.byID[u.GRN.String()]; held {
		return pgxerr.Classify(&pgconn.PgError{
			Severity:       "ERROR",
			Code:           "23505",
			Message:        `duplicate key value violates unique constraint "users_pkey"`,
			ConstraintName: "users_pkey",
			TableName:      "users",
		}, "create", nil, nil)
	}
	s.byID[u.GRN.String()] = u
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

// Delete removes a user, as a registration that could not finish
// does with the one it created.
func (s *fakeUserStore) Delete(_ context.Context, g grn.GRN) error {
	s.remove(g)
	return nil
}

// grantRoles replaces the stored user with one carrying the given
// direct roles; login creates users without any.
func (s *fakeUserStore) grantRoles(g grn.GRN, roles ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[g.String()] = user.New(g, s.byID[g.String()].Nickname, roles...)
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

// Refresh answers every refresh token not live: the double holds no
// CLI session, which the integration tests exercise against Postgres.
func (s *fakeSessionStore) Refresh(context.Context, string, authn.CLITokens) (*authn.Session, error) {
	return nil, authn.ErrRefreshTokenNotLive
}

func (s *fakeSessionStore) List(_ context.Context, user grn.GRN) ([]*authn.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*authn.Session
	for _, sess := range s.byID {
		if sess.User == user {
			out = append(out, sess)
		}
	}
	return out, nil
}

func (s *fakeSessionStore) Revoke(_ context.Context, user grn.GRN, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.byID {
		if sess.User == user && sess.ID == id {
			delete(s.byID, token)
			return nil
		}
	}
	return authn.ErrSessionNotFound
}

// fakeDeviceStore is an authn.DeviceStore double: it records what was
// issued and answers every poll with poll.
type fakeDeviceStore struct {
	mu     sync.Mutex
	issued []string
	poll   authn.PollResult
}

func (s *fakeDeviceStore) Issue(_ context.Context, grant authn.DeviceGrant, clientName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issued = append(s.issued, clientName)
	return nil
}

func (s *fakeDeviceStore) Decide(context.Context, string, grn.GRN, bool) error {
	return authn.ErrDeviceCodeNotPending
}

func (s *fakeDeviceStore) Poll(context.Context, string, authn.CLITokens) (authn.PollResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poll.Outcome == "" {
		return authn.PollResult{}, authn.ErrDeviceCodeUnknown
	}
	return s.poll, nil
}

// fakeOAuthStateStore is an in-memory authn.OAuthStateStore double. It
// holds each live state's expiry, as the table holds a row's.
type fakeOAuthStateStore struct {
	mu       sync.Mutex
	expiryOf map[string]time.Time
	pathOf   map[string]string
}

func newFakeOAuthStateStore() *fakeOAuthStateStore {
	return &fakeOAuthStateStore{expiryOf: map[string]time.Time{}, pathOf: map[string]string{}}
}

func (s *fakeOAuthStateStore) Issue(_ context.Context, state string, ttl time.Duration, returnPath string) error {
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
	s.pathOf[state] = returnPath
	return nil
}

func (s *fakeOAuthStateStore) Consume(_ context.Context, state string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.expiryOf[state]
	if !ok || !time.Now().UTC().Before(expiry) {
		return "", authn.ErrOAuthStateNotLive
	}
	returnPath := s.pathOf[state]
	delete(s.expiryOf, state)
	delete(s.pathOf, state)
	return returnPath, nil
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
	claims         int
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
	s.orgs[o.GRN.String()] = o
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
	name := o.Name
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
		if m.User == userGRN {
			out = append(out, s.orgs[m.Organization.String()])
		}
	}
	return out, nil
}

func (s *fakeOrgStore) CreateGroup(_ context.Context, g *org.Group) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups[g.GRN.String()] = g
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
		if gr.Organization == orgGRN {
			out = append(out, gr)
		}
	}
	slices.SortFunc(out, func(a, b *org.Group) int {
		return strings.Compare(a.GRN.String(), b.GRN.String())
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
	name := gr.Name
	if p.Name != nil {
		name = *p.Name
	}
	roles := gr.Roles
	if p.Roles != nil {
		roles = p.Roles
	}
	updated := org.NewGroup(g, name, gr.Organization, roles...)
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
		if !ok || gr.Organization != orgGRN || !slices.Contains(members, memberGRN) {
			continue
		}
		out = append(out, gr.GRN)
	}
	slices.SortFunc(out, func(a, b grn.GRN) int { return strings.Compare(a.String(), b.String()) })
	return out, nil
}

// ListMemberGroups ignores paging, as ListGroups here does.
func (s *fakeOrgStore) ListMemberGroups(ctx context.Context, orgGRN, memberGRN grn.GRN, _ listfilter.Filter) ([]*org.Group, error) {
	grns, err := s.ListGroupsByMember(ctx, orgGRN, memberGRN)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*org.Group, 0, len(grns))
	for _, g := range grns {
		out = append(out, s.groups[g.String()])
	}
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
	s.projects[p.GRN.String()] = p
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
		if p.Organization == orgGRN {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b *org.Project) int {
		return strings.Compare(a.GRN.String(), b.GRN.String())
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
	name := p.Name
	if patch.Name != nil {
		name = *patch.Name
	}
	roles := p.Roles
	if patch.Roles != nil {
		roles = patch.Roles
	}
	updated := org.NewProject(g, name, p.Organization, roles...)
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

func (s *fakeOrgStore) ListProjectsByMember(_ context.Context, orgGRN, memberGRN grn.GRN) ([]grn.GRN, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []grn.GRN{}
	for key, members := range s.projectMembers {
		p, ok := s.projects[key]
		if !ok || p.Organization != orgGRN || !slices.Contains(members, memberGRN) {
			continue
		}
		out = append(out, p.GRN)
	}
	slices.SortFunc(out, func(a, b grn.GRN) int { return strings.Compare(a.String(), b.String()) })
	return out, nil
}

func (s *fakeOrgStore) AddMember(_ context.Context, m *org.Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memberKey(m.Organization, m.User)
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
		if m.Organization == orgGRN {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *fakeOrgStore) CreateInvitation(_ context.Context, inv *org.Invitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invitations[inv.Token] = inv
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

func (s *fakeOrgStore) AcceptInvitation(_ context.Context, token string, userGRN grn.GRN) (*org.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, err := s.claimLocked(token)
	if err != nil {
		return nil, err
	}
	m := org.NewMembership(inv.Organization, userGRN, inv.Role)
	k := memberKey(m.Organization, m.User)
	if _, ok := s.members[k]; ok {
		return nil, org.ErrMemberAlreadyExists
	}
	s.members[k] = m
	s.accepted[token] = true
	return m, nil
}

// ClaimInvitation spends the invitation as the store's claim does. The
// double keeps no mark, so the Invitation is returned as created.
func (s *fakeOrgStore) ClaimInvitation(_ context.Context, token string) (*org.Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, err := s.claimLocked(token)
	if err != nil {
		return nil, err
	}
	s.accepted[token] = true
	s.claims++
	return inv, nil
}

// claimed reports how many registration claims succeeded, released
// ones included.
func (s *fakeOrgStore) claimed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claims
}

func (s *fakeOrgStore) CompleteInvitationClaim(context.Context, *org.Invitation, grn.GRN) error {
	return nil
}

func (s *fakeOrgStore) ReleaseInvitationClaim(_ context.Context, inv *org.Invitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accepted[inv.Token] = false
	return nil
}

// claimLocked answers the invitation a claim on token would spend, or
// the refusal the store gives, in the store's order.
func (s *fakeOrgStore) claimLocked(token string) (*org.Invitation, error) {
	inv, ok := s.invitations[token]
	switch {
	case !ok:
		return nil, org.ErrInvitationNotFound
	case s.accepted[token]:
		return nil, org.ErrInvitationAccepted
	case inv.Expired(time.Now().UTC()):
		return nil, org.ErrInvitationExpired
	}
	return inv, nil
}

// spent reports whether an accept or a claim holds the invitation.
func (s *fakeOrgStore) spent(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted[token]
}

// fakeAgentStore is an in-memory agent.Store double. The API reads
// agents and the assignments holding them; the write methods an
// operator reaches over the machine listener stay unimplemented and
// panic through the embedded interface.
type fakeAgentStore struct {
	agent.Store
	mu          sync.Mutex
	agents      map[string]*agent.Agent
	assignments map[string]*agent.Assignment
}

func newFakeAgentStore() *fakeAgentStore {
	return &fakeAgentStore{
		agents:      map[string]*agent.Agent{},
		assignments: map[string]*agent.Assignment{},
	}
}

func (s *fakeAgentStore) Create(_ context.Context, a *agent.Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[a.GRN.String()] = a
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
		if a.Organization == orgGRN {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b *agent.Agent) int {
		return strings.Compare(a.GRN.String(), b.GRN.String())
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
		epoch = held.Epoch + 1
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

// LatestGeneration answers what a store holding no activation does:
// the fake records none, so every agent is one never activated.
func (s *fakeAgentStore) LatestGeneration(context.Context, grn.GRN) (*agent.Generation, error) {
	return nil, nil
}

// LatestGenerationsByOrg answers LatestGeneration for every agent.
func (s *fakeAgentStore) LatestGenerationsByOrg(context.Context, grn.GRN) (map[grn.GRN]agent.Generation, error) {
	return map[grn.GRN]agent.Generation{}, nil
}

func (s *fakeAgentStore) ListAssignmentsByOrg(_ context.Context, orgGRN grn.GRN) ([]*agent.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*agent.Assignment{}
	for key, a := range s.assignments {
		if held, ok := s.agents[key]; ok && held.Organization == orgGRN {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b *agent.Assignment) int {
		return strings.Compare(a.Agent.String(), b.Agent.String())
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
	return &message.Consent{OrgA: a, OrgB: b, AConsentedAt: aAt, BConsentedAt: bAt}, nil
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
		out = append(out, &message.Consent{OrgA: a, OrgB: b, AConsentedAt: aAt, BConsentedAt: bAt})
	}
	slices.SortFunc(out, func(x, y *message.Consent) int {
		return strings.Compare(x.OrgA.String()+x.OrgB.String(), y.OrgA.String()+y.OrgB.String())
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

func (r *fakeTurnEvents) Announce(_ context.Context, agent grn.GRN, e watch.Event, _ *watch.Fence) error {
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
	createRefused  bool
}

func newFakePasswordCredentialStore() *fakePasswordCredentialStore {
	return &fakePasswordCredentialStore{byUser: map[string]authn.Verifier{}}
}

// Create records the first verifier for a user. A second one is a
// conflict, as the table's primary key makes it.
func (s *fakePasswordCredentialStore) Create(_ context.Context, userGRN grn.GRN, v authn.Verifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createRefused {
		return errorx.New(errorx.KindUnavailable, "password credential store is gone")
	}
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

// refuseCreates stands in, while on, for a store that is gone by the
// time a registration writes the credential.
func (s *fakePasswordCredentialStore) refuseCreates(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createRefused = on
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
	// keys holds each operator's keys by GRN, and issuances the
	// rotation each issuance was asked under.
	keys      map[string][]operator.Key
	issuances []operator.KeyRotation
}

func newFakeOperatorStore() *fakeOperatorStore {
	return &fakeOperatorStore{
		operators: map[string]*operator.Operator{},
		keys:      map[string][]operator.Key{},
	}
}

// IssueKey applies the rotation as the real store does, over keys
// held in memory: first refuses any key ever issued, withdrawn or not,
// planned a third live one, and immediate withdraws every live one
// before recording the new key.
func (s *fakeOperatorStore) IssueKey(_ context.Context, k *operator.IssuedKey, rotation operator.KeyRotation) (operator.Key, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := k.Operator().String()
	if _, ok := s.operators[g]; !ok {
		return operator.Key{}, operator.ErrOperatorNotFound
	}
	s.issuances = append(s.issuances, rotation)
	live := 0
	for _, held := range s.keys[g] {
		if held.State != operator.KeyStateRevoked {
			live++
		}
	}
	switch rotation {
	case operator.KeyRotationFirst:
		if len(s.keys[g]) > 0 {
			return operator.Key{}, operator.ErrOperatorWasKeyed
		}
	case operator.KeyRotationPlanned:
		if live >= operator.LiveKeysPerOperator {
			return operator.Key{}, operator.ErrKeyLimit
		}
		for i := range s.keys[g] {
			if s.keys[g][i].State == operator.KeyStateActive {
				s.keys[g][i].State = operator.KeyStateRetiring
			}
		}
	case operator.KeyRotationImmediate:
		for i := range s.keys[g] {
			s.keys[g][i].State = operator.KeyStateRevoked
		}
	}
	jwk, err := k.PublicJWK()
	if err != nil {
		return operator.Key{}, err
	}
	public, err := operator.ParsePublicJWK(jwk, k.KID())
	if err != nil {
		return operator.Key{}, err
	}
	key := operator.Key{ID: int64(len(s.issuances)), Operator: k.Operator(), KID: k.KID(),
		Algorithm: operator.KeyAlgorithm, Public: public, IssuedBy: k.IssuedBy(), IssuedAt: k.IssuedAt(),
		State: operator.KeyStateActive}
	s.keys[g] = append(s.keys[g], key)
	return key, nil
}

func (s *fakeOperatorStore) ListKeys(_ context.Context, g grn.GRN) ([]operator.Key, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.operators[g.String()]; !ok {
		return nil, operator.ErrOperatorNotFound
	}
	return append([]operator.Key(nil), s.keys[g.String()]...), nil
}

func (s *fakeOperatorStore) RevokeKey(_ context.Context, g grn.GRN, kid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, held := range s.keys[g.String()] {
		if held.KID == kid {
			s.keys[g.String()][i].State = operator.KeyStateRevoked
			return nil
		}
	}
	return operator.ErrKeyNotFound
}

// Register writes the record, as the statement does, and answers the
// operator it stored. The real store reads that back inside the same
// transaction; this one answers the operator it stored.
func (s *fakeOperatorStore) Register(_ context.Context, o *operator.Operator) (*operator.Operator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.operators[o.GRN.String()]; ok {
		return nil, operator.ErrOperatorAlreadyRegistered
	}
	s.operators[o.GRN.String()] = o
	return o, nil
}

// Delete removes the record, as the statement does.
func (s *fakeOperatorStore) Delete(_ context.Context, g grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.operators[g.String()]; !ok {
		return operator.ErrOperatorNotFound
	}
	delete(s.operators, g.String())
	return nil
}

// UpdateDetails replaces the record with one carrying the patched
// details, keeping what the patch left out. It touches no token, as
// the statement touches no other table.
func (s *fakeOperatorStore) UpdateDetails(_ context.Context, g grn.GRN, patch operator.DetailsPatch) (*operator.Operator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.operators[g.String()]
	if !ok {
		return nil, operator.ErrOperatorNotFound
	}
	name, description := o.Name, o.Description
	if n := patch.Name(); n != nil {
		name = *n
	}
	if d := patch.Description(); d != nil {
		description = *d
	}
	merged, err := operator.NewDetailsPatch(&name, &description)
	if err != nil {
		return nil, err
	}
	updated, err := operator.New(o.Organization, g.Identifier, o.RegisteredBy, merged)
	if err != nil {
		return nil, err
	}
	updated.ControlEndpoint = o.ControlEndpoint
	s.operators[g.String()] = updated
	return updated, nil
}

// SetControlEndpoint replaces the endpoint on a copy of the record,
// leaving everything else as it stands.
func (s *fakeOperatorStore) SetControlEndpoint(_ context.Context, g grn.GRN, endpoint *operator.ControlEndpoint) (*operator.Operator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.operators[g.String()]
	if !ok {
		return nil, operator.ErrOperatorNotFound
	}
	updated := *o
	updated.ControlEndpoint = endpoint
	s.operators[g.String()] = &updated
	return &updated, nil
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
		if o.Organization == orgGRN {
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b *operator.Operator) int {
		return strings.Compare(a.GRN.String(), b.GRN.String())
	})
	return out, nil
}

// CountOrgContents counts what this fake holds for the organization.
// It counts the same maps the List methods read, so a test that writes
// rows through the fake and then previews sees its own writes.
func (s *fakeOrgStore) CountOrgContents(_ context.Context, orgGRN grn.GRN) (org.Contents, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c org.Contents
	for _, gr := range s.groups {
		if gr.Organization == orgGRN {
			c.Groups++
		}
	}
	for _, p := range s.projects {
		if p.Organization == orgGRN {
			c.Projects++
		}
	}
	for _, m := range s.members {
		if m.Organization == orgGRN {
			c.Members++
		}
	}
	for _, inv := range s.invitations {
		if inv.Organization == orgGRN {
			c.Invitations++
		}
	}
	return c, nil
}

// CountByOrg counts the organization's agents.
func (s *fakeAgentStore) CountByOrg(_ context.Context, orgGRN grn.GRN) (agent.OrgCounts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c agent.OrgCounts
	for _, a := range s.agents {
		if a.Organization == orgGRN {
			c.Agents++
		}
	}
	return c, nil
}

// CountByOrg counts the organization's registered operators.
func (s *fakeOperatorStore) CountByOrg(_ context.Context, orgGRN grn.GRN) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, o := range s.operators {
		if o.Organization == orgGRN {
			n++
		}
	}
	return n, nil
}

// CountByOrg counts the relations the organization is a side of,
// reading the same map List does so both sides of a pair are counted
// once from either organization.
func (s *fakeConsentStore) CountByOrg(ctx context.Context, orgGRN grn.GRN) (int, error) {
	relations, err := s.List(ctx, orgGRN)
	if err != nil {
		return 0, err
	}
	return len(relations), nil
}

// fakeAuthorityReader stands in for the certificate authority the
// composition root reads through (api.AuthorityReader). A test sets
// which organizations hold one; the default is that none does, which
// is the state of an organization that has needed no certificate.
type fakeAuthorityReader struct {
	mu    sync.Mutex
	holds map[string]bool
	err   error
}

func newFakeAuthorityReader() *fakeAuthorityReader {
	return &fakeAuthorityReader{holds: map[string]bool{}}
}

func (s *fakeAuthorityReader) give(orgGRN grn.GRN) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.holds[orgGRN.String()] = true
}

func (s *fakeAuthorityReader) HasAuthority(_ context.Context, orgGRN grn.GRN) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	return s.holds[orgGRN.String()], nil
}
