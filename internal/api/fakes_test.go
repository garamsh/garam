package api_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
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

func (s *fakeUserStore) Create(_ context.Context, u *user.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.expiryOf[state] = time.Now().UTC().Add(ttl)
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

func (s *fakeOrgStore) ListOrgsByMember(_ context.Context, userGRN grn.GRN, _ org.ListFilter) ([]*org.Organization, error) {
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

func (s *fakeOrgStore) ListGroups(_ context.Context, orgGRN grn.GRN, _ org.ListFilter) ([]*org.Group, error) {
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

func (s *fakeOrgStore) ListProjects(_ context.Context, orgGRN grn.GRN, _ org.ListFilter) ([]*org.Project, error) {
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

func (s *fakeAgentStore) ListByOrg(_ context.Context, orgGRN grn.GRN) ([]*agent.Agent, error) {
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

// fakeWatchLeases is a watch.Store in memory: one entry per open watch,
// which is what says whether a watch outlived the stream it belonged to.
type fakeWatchLeases struct {
	mu   sync.Mutex
	byID map[watch.WatchID]grn.GRN
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
	delete(l.byID, id)
	return nil
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
