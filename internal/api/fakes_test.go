package api_test

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/garamsh/gaon/internal/authn"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
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
