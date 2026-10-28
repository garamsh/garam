package authz_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/wire"
)

// fakeStore is an in-memory authz.Store that answers from what it
// holds, in the order it was written, with the refusals the tests
// exercise. A *Policy it returns carries the GRN, org and name it was
// created with: its status and default version are unexported, so the
// store keeps those in its own records.
type fakeStore struct {
	mu       sync.Mutex
	clock    time.Time
	policies []*fakePolicy
}

type fakePolicy struct {
	policy         *authz.Policy
	status         wire.PolicyStatus
	defaultVersion int
	versions       []*authz.PolicyVersion
	attachments    []*authz.PolicyAttachment
}

var _ authz.Store = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{clock: time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)}
}

// seed stores a policy over doc in status, attached to subjects, and
// returns its GRN.
func (s *fakeStore) seed(t *testing.T, orgGRN grn.GRN, status wire.PolicyStatus, doc authz.PolicyDocument, subjects ...grn.GRN) grn.GRN {
	t.Helper()
	ctx := context.Background()
	g := policyGRN(t, orgGRN)
	_, err := s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), doc, subjectGRN("author"))
	require.NoError(t, err)
	for _, subject := range subjects {
		_, err := s.AttachPolicy(ctx, g, subject)
		require.NoError(t, err)
	}
	_, err = s.SetPolicyStatus(ctx, g, status)
	require.NoError(t, err)
	return g
}

// tick returns the time a write is recorded at, a second after the
// last one, so two records never share a timestamp.
func (s *fakeStore) tick() time.Time {
	s.clock = s.clock.Add(time.Second)
	return s.clock
}

func (s *fakeStore) find(g grn.GRN) (*fakePolicy, error) {
	for _, p := range s.policies {
		if p.policy.GRN == g {
			return p, nil
		}
	}
	return nil, authz.ErrPolicyNotFound
}

func (s *fakeStore) CreatePolicy(_ context.Context, p *authz.Policy, doc authz.PolicyDocument, createdBy grn.GRN) (*authz.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies = append(s.policies, &fakePolicy{
		policy:         p,
		status:         p.Status,
		defaultVersion: p.DefaultVersion,
		versions:       []*authz.PolicyVersion{{Policy: p.GRN, Version: p.DefaultVersion, Document: doc, CreatedBy: createdBy, CreatedAt: s.tick()}},
	})
	return p, nil
}

func (s *fakeStore) GetPolicy(_ context.Context, g grn.GRN) (*authz.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(g)
	if err != nil {
		return nil, err
	}
	return p.policy, nil
}

func (s *fakeStore) ListPolicies(_ context.Context, orgGRN grn.GRN) ([]*authz.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*authz.Policy
	for _, p := range s.policies {
		if p.policy.Organization == orgGRN {
			out = append(out, p.policy)
		}
	}
	return out, nil
}

// SearchPolicies matches as the query does: the name or the
// identifier holds the needle, case aside.
func (s *fakeStore) SearchPolicies(_ context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*authz.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	needle := strings.ToLower(n.Text())
	var out []*authz.Policy
	for _, p := range s.policies {
		name, id := strings.ToLower(p.policy.Name), strings.ToLower(p.policy.GRN.Identifier)
		if p.policy.Organization == orgGRN && (strings.Contains(name, needle) || strings.Contains(id, needle)) {
			out = append(out, p.policy)
		}
	}
	return out, nil
}

func (s *fakeStore) AddPolicyVersion(_ context.Context, g grn.GRN, doc authz.PolicyDocument, createdBy grn.GRN) (*authz.PolicyVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(g)
	if err != nil {
		return nil, err
	}
	v := &authz.PolicyVersion{Policy: g, Version: len(p.versions) + 1, Document: doc, CreatedBy: createdBy, CreatedAt: s.tick()}
	p.versions = append(p.versions, v)
	return v, nil
}

func (s *fakeStore) ListPolicyVersions(_ context.Context, g grn.GRN) ([]*authz.PolicyVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(g)
	if err != nil {
		return nil, nil
	}
	return slices.Clone(p.versions), nil
}

func (s *fakeStore) SetPolicyStatus(_ context.Context, g grn.GRN, status wire.PolicyStatus) (*authz.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(g)
	if err != nil {
		return nil, err
	}
	p.status = status
	return p.policy, nil
}

func (s *fakeStore) DeletePolicy(_ context.Context, g grn.GRN) (*authz.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(g)
	if err != nil {
		return nil, err
	}
	s.policies = slices.DeleteFunc(s.policies, func(q *fakePolicy) bool { return q == p })
	return p.policy, nil
}

func (s *fakeStore) SetPolicyDefaultVersion(_ context.Context, g grn.GRN, version int) (*authz.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(g)
	if err != nil || version < 1 || version > len(p.versions) {
		return nil, authz.ErrPolicyVersionNotFound
	}
	p.defaultVersion = version
	return p.policy, nil
}

func (s *fakeStore) AttachPolicy(_ context.Context, policyGRN, subjectGRN grn.GRN) (*authz.PolicyAttachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(policyGRN)
	if err != nil {
		return nil, err
	}
	a := &authz.PolicyAttachment{Policy: policyGRN, Subject: subjectGRN, CreatedAt: s.tick()}
	p.attachments = append(p.attachments, a)
	return a, nil
}

func (s *fakeStore) DetachPolicy(_ context.Context, policyGRN, subjectGRN grn.GRN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(policyGRN)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(p.attachments, func(a *authz.PolicyAttachment) bool { return a.Subject == subjectGRN })
	if i < 0 {
		return authz.ErrAttachmentNotFound
	}
	if p.status == wire.PolicyStatusActive && len(p.attachments) == 1 {
		return authz.ErrLastAttachment
	}
	p.attachments = slices.Delete(p.attachments, i, i+1)
	return nil
}

func (s *fakeStore) ListPolicyAttachments(_ context.Context, policyGRN grn.GRN) ([]*authz.PolicyAttachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.find(policyGRN)
	if err != nil {
		return nil, nil
	}
	return slices.Clone(p.attachments), nil
}

func (s *fakeStore) ActivePolicies(_ context.Context, orgGRN grn.GRN) ([]authz.ActivePolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []authz.ActivePolicy
	for _, p := range s.policies {
		if p.policy.Organization != orgGRN || p.status != wire.PolicyStatusActive {
			continue
		}
		subjects := make([]grn.GRN, 0, len(p.attachments))
		for _, a := range p.attachments {
			subjects = append(subjects, a.Subject)
		}
		out = append(out, authz.ActivePolicy{GRN: p.policy.GRN, Document: p.versions[p.defaultVersion-1].Document, Subjects: subjects})
	}
	return out, nil
}
