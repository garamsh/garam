package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
	"github.com/garamsh/garam/platform/internal/signer"
)

const deliveryIssuer = "garam-platform-test"

// fakePolicy is the policy service at its gRPC boundary. It verifies
// the bearer token as the service's verifier does, records each
// ApplyEntityChanges call, and answers from fields a case sets.
type fakePolicy struct {
	policyv1.UnimplementedPolicyServiceServer
	jwks []byte

	mu      sync.Mutex
	calls   []*policyv1.ApplyEntityChangesRequest
	refuse  map[string]string // entity id -> reason
	fail    error
	onApply func()

	policies  map[string]*fakeLink // policy grn -> role link
	failLinks error
	// failOnly names the one link call that fails; empty fails all.
	failOnly string
	// refuseOrg is the error each link call answers for an organization,
	// by its GRN, while the others answer as they would.
	refuseOrg map[string]error
	// refuseOwner is the error CreatePolicy answers for a scoped link whose
	// principal is the owner, by its GRN, while the others answer as they
	// would.
	refuseOwner map[string]error
}

// linkRefuseOwner makes CreatePolicy of the scoped link of owner fail with
// err while it is set, and none of another owner's. A nil err ends it.
func (f *fakePolicy) linkRefuseOwner(owner string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuseOwner == nil {
		f.refuseOwner = map[string]error{}
	}
	f.refuseOwner[owner] = err
}

// linkRefuseOrg makes every link call for org fail with err while it is
// set, and none for another organization. A nil err ends it.
func (f *fakePolicy) linkRefuseOrg(org string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuseOrg == nil {
		f.refuseOrg = map[string]error{}
	}
	f.refuseOrg[org] = err
}

func (f *fakePolicy) ApplyEntityChanges(
	_ context.Context, req *policyv1.ApplyEntityChangesRequest,
) (*policyv1.ApplyEntityChangesResponse, error) {
	if f.onApply != nil {
		f.onApply()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.fail != nil {
		return nil, f.fail
	}
	resp := &policyv1.ApplyEntityChangesResponse{}
	for i, c := range req.GetChanges() {
		if reason, ok := f.refuse[c.GetEntity().GetId()]; ok && c.GetUpsert() != nil {
			resp.Refusals = append(resp.Refusals,
				&policyv1.EntityChangeRefusal{Index: uint32(i), Error: reason})
			continue
		}
		resp.Written++
	}
	return resp, nil
}

// fakeLink is one role-link policy as the fake holds it.
type fakeLink struct {
	req    *policyv1.CreatePolicyRequest
	active bool
}

// linkFail makes every link call fail with err while it is set.
func (f *fakePolicy) linkFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failLinks, f.failOnly = err, ""
}

// linkFailOn makes only the named link call (CreatePolicy,
// ActivatePolicy or DeletePolicy) fail with err while it is set; the
// others answer as they would.
func (f *fakePolicy) linkFailOn(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failLinks, f.failOnly = err, method
}

// linkErr is what a call to method answers when the case has set it to
// fail. The caller holds the lock.
func (f *fakePolicy) linkErr(method, org string) error {
	if err := f.refuseOrg[org]; err != nil {
		return err
	}
	if f.failOnly != "" && f.failOnly != method {
		return nil
	}
	return f.failLinks
}

func (f *fakePolicy) CreatePolicy(
	_ context.Context, req *policyv1.CreatePolicyRequest,
) (*policyv1.CreatePolicyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.linkErr("CreatePolicy", req.GetOrg()); err != nil {
		return nil, err
	}
	if err := f.refuseOwner[req.GetBody().GetRoleLink().GetPrincipal().GetId()]; err != nil {
		return nil, err
	}
	if _, ok := f.policies[req.GetPolicy()]; ok {
		return nil, status.Error(codes.AlreadyExists, "policy exists")
	}
	if f.policies == nil {
		f.policies = map[string]*fakeLink{}
	}
	f.policies[req.GetPolicy()] = &fakeLink{req: req}
	return &policyv1.CreatePolicyResponse{}, nil
}

func (f *fakePolicy) ActivatePolicy(
	_ context.Context, req *policyv1.ActivatePolicyRequest,
) (*policyv1.ActivatePolicyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.linkErr("ActivatePolicy", req.GetOrg()); err != nil {
		return nil, err
	}
	l, ok := f.policies[req.GetPolicy()]
	if !ok {
		return nil, status.Error(codes.NotFound, "no policy")
	}
	l.active = true
	return &policyv1.ActivatePolicyResponse{}, nil
}

func (f *fakePolicy) DeletePolicy(
	_ context.Context, req *policyv1.DeletePolicyRequest,
) (*policyv1.DeletePolicyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.linkErr("DeletePolicy", req.GetOrg()); err != nil {
		return nil, err
	}
	if _, ok := f.policies[req.GetPolicy()]; !ok {
		return nil, status.Error(codes.NotFound, "no policy")
	}
	delete(f.policies, req.GetPolicy())
	return &policyv1.DeletePolicyResponse{}, nil
}

// links is the role links in force as "ROLE principal resource", the
// way the service reads them: an active policy and nothing else.
func (f *fakePolicy) links() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, l := range f.policies {
		if !l.active {
			continue
		}
		rl := l.req.GetBody().GetRoleLink()
		out = append(out, strings.TrimPrefix(rl.GetRole().String(), "ROLE_")+" "+
			rl.GetPrincipal().GetId()+" "+rl.GetResource().GetId())
	}
	slices.Sort(out)
	return out
}

func (f *fakePolicy) received() []*policyv1.ApplyEntityChangesRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// verify checks the token as ADR-0138's profile has the service do.
func (f *fakePolicy) verify(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	auth := md.Get("authorization")
	if len(auth) != 1 || !strings.HasPrefix(auth[0], "Bearer ") {
		return status.Error(codes.Unauthenticated, "no bearer token")
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(f.jwks, &set); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	sig, err := jose.ParseSigned(strings.TrimPrefix(auth[0], "Bearer "),
		[]jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return status.Error(codes.Unauthenticated, "unparseable token")
	}
	if len(sig.Signatures) != 1 || len(set.Key(sig.Signatures[0].Header.KeyID)) != 1 {
		return status.Error(codes.Unauthenticated, "unknown key")
	}
	body, err := sig.Verify(set.Key(sig.Signatures[0].Header.KeyID)[0])
	if err != nil {
		return status.Error(codes.Unauthenticated, "bad signature")
	}
	var claims struct {
		Iss, Sub, Aud string
		Iat, Exp      int64
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return status.Error(codes.Unauthenticated, "bad claims")
	}
	now := time.Now().Unix()
	if claims.Iss != deliveryIssuer || claims.Sub != "platform" || claims.Aud != "policy" ||
		claims.Exp <= now || claims.Exp-claims.Iat > 300 {
		return status.Error(codes.Unauthenticated, "token outside the profile")
	}
	return nil
}

func newKey(t *testing.T) (*signer.Key, []byte) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	jwks, err := key.JWKS()
	require.NoError(t, err)
	return key, jwks
}

// serveFakePolicy runs the fake over TLS on a loopback port and
// returns its address and the CA file that verifies it.
func serveFakePolicy(t *testing.T, f *fakePolicy) (addr, caFile string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "policy"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	caFile = filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewServerTLSFromCert(&tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv})),
		grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			if err := f.verify(ctx); err != nil {
				return nil, err
			}
			return h(ctx, req)
		}))
	policyv1.RegisterPolicyServiceServer(srv, f)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), caFile
}

// deliverer is the platform's delivery store and sender wired as
// serve api wires them, against the fake.
type deliverer struct {
	store  *policyfeedpg.Store
	sender policyfeed.Sender
}

func newDeliverer(t *testing.T, p *pgxpool.Pool, f *fakePolicy, signKey *signer.Key) deliverer {
	t.Helper()
	addr, caFile := serveFakePolicy(t, f)
	conn, err := policyfeedgrpc.Dial(addr, caFile, deliveryIssuer, signKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return deliverer{
		store:  policyfeedpg.NewStore(p),
		sender: policyfeedgrpc.NewSender(policyv1.NewPolicyServiceClient(conn)),
	}
}

func (d deliverer) pass(t *testing.T) (policyfeed.Result, error) {
	t.Helper()
	return d.store.Deliver(context.Background(), policyfeed.BatchLimit, d.sender)
}

func newFake(t *testing.T) (*fakePolicy, *signer.Key) {
	t.Helper()
	key, jwks := newKey(t)
	return &fakePolicy{jwks: jwks}, key
}

// changesByEntity indexes a request's changes as "Type id".
func changesByEntity(req *policyv1.ApplyEntityChangesRequest) map[string]*policyv1.EntityChange {
	out := map[string]*policyv1.EntityChange{}
	for _, c := range req.GetChanges() {
		out[c.GetEntity().GetType()+" "+c.GetEntity().GetId()] = c
	}
	return out
}

func parentKeys(c *policyv1.EntityChange) []string {
	var out []string
	for _, p := range c.GetUpsert().GetParents() {
		out = append(out, p.GetType()+" "+p.GetId())
	}
	return out
}

func TestPolicyDelivery_SendsWhatTheOwnersTablesSayAndDeletesTheMarks(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	startFeed(t, p)
	seedOutboxOrg(t, p)
	require.Equal(t, outboxEntities, markKeys(outboxMarks(t, p)))

	result, err := d.pass(t)
	require.NoError(t, err)

	assert.Equal(t, len(outboxEntities), result.Marks)
	assert.Empty(t, result.Refused)
	assert.Empty(t, outboxMarks(t, p), "a delivered mark is gone")
	calls := fake.received()
	require.Len(t, calls, 1)
	changes := changesByEntity(calls[0])
	require.Len(t, changes, len(outboxEntities)-4, "the role links are not entity changes")
	assert.Equal(t, []string{"MEMBER " + outboxUser + " " + outboxOrg}, fake.links(),
		"the seeded membership reached the service as a link")
	seqs := map[int64]bool{}
	for _, c := range changes {
		seqs[c.GetSequence()] = true
	}
	assert.Len(t, seqs, 1, "one pass takes one sequence")

	org := changes["Iam::Org "+outboxOrg].GetUpsert()
	assert.Equal(t, outboxOrg, org.GetOrg().GetId())
	assert.Equal(t, "Iam::OrgMembers", org.GetMembers().GetType())
	assert.Equal(t, []string{
		"Iam::OrgMembers " + outboxOrg, "Iam::Project " + outboxProject, "Iam::Team " + outboxTeam,
	}, parentKeys(changes["Iam::User "+outboxUser]))
	assert.Equal(t, outboxOrg, changes["Iam::Team "+outboxTeam].GetUpsert().GetOrg().GetId())
}

func TestPolicyDelivery_SequenceRisesAcrossPasses(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	seedWithFeedOn(t, p)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)
	_, err := d.pass(t)
	require.NoError(t, err)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)
	_, err = d.pass(t)
	require.NoError(t, err)

	calls := fake.received()
	require.Len(t, calls, 2)
	assert.Greater(t, calls[1].GetChanges()[0].GetSequence(), calls[0].GetChanges()[0].GetSequence())
}

func TestPolicyDelivery_AnEntityTheOwnersNoLongerHoldIsADelete(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	seedWithFeedOn(t, p)
	execSQL(t, p, `DELETE FROM project_members WHERE project_grn = $1`, outboxProject)
	execSQL(t, p, `DELETE FROM projects WHERE grn = $1`, outboxProject)
	require.Contains(t, outboxMarks(t, p), "Iam::Project "+outboxProject)

	_, err := d.pass(t)
	require.NoError(t, err)

	change := changesByEntity(fake.received()[0])["Iam::Project "+outboxProject]
	require.NotNil(t, change)
	assert.NotNil(t, change.GetDelete())
	assert.Nil(t, change.GetUpsert())
}

func TestPolicyDelivery_AWriteLandingMidPassSurvivesIt(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	seedWithFeedOn(t, p)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam2)
	fake.onApply = func() {
		// A write to one team commits between the read and the delete.
		execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)
	}

	_, err := d.pass(t)
	require.NoError(t, err)

	marks := outboxMarks(t, p)
	assert.Equal(t, map[string]int64{"Iam::Team " + outboxTeam: 2}, marks,
		"the mark written mid-pass stays at its new generation, and the other goes")

	fake.onApply = nil
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Empty(t, outboxMarks(t, p))
	calls := fake.received()
	require.Len(t, calls, 2)
	assert.Greater(t, calls[1].GetChanges()[0].GetSequence(), calls[0].GetChanges()[0].GetSequence(),
		"the surviving mark goes out at a higher sequence")
}

func TestPolicyDelivery_ARefusedEntityIsDeletedAtAHigherSequence(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	fake.refuse = map[string]string{outboxTeam: "too many parents"}
	d := newDeliverer(t, p, fake, key)
	seedWithFeedOn(t, p)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam2)

	result, err := d.pass(t)
	require.NoError(t, err)

	assert.Equal(t, []policyfeed.Refusal{{
		Entity: policyfeed.Entity{Type: "Iam::Team", ID: outboxTeam}, Reason: "too many parents",
	}}, result.Refused)
	calls := fake.received()
	require.Len(t, calls, 2)
	require.Len(t, calls[1].GetChanges(), 1)
	del := calls[1].GetChanges()[0]
	assert.Equal(t, outboxTeam, del.GetEntity().GetId())
	assert.NotNil(t, del.GetDelete())
	assert.Greater(t, del.GetSequence(), calls[0].GetChanges()[0].GetSequence())
	assert.Empty(t, outboxMarks(t, p), "a refused entity's mark goes too, or it would be refused forever")
}

func TestPolicyDelivery_AFailedCallLeavesEveryMark(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	fake.fail = status.Error(codes.Unavailable, "down")
	d := newDeliverer(t, p, fake, key)
	seedWithFeedOn(t, p)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)

	_, err := d.pass(t)
	require.Error(t, err)
	assert.Equal(t, map[string]int64{"Iam::Team " + outboxTeam: 1}, outboxMarks(t, p))

	fake.mu.Lock()
	fake.fail = nil
	fake.mu.Unlock()
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Empty(t, outboxMarks(t, p), "the next pass redelivers it")
}

func TestPolicyDelivery_ATokenTheServiceRefusesDeliversNothing(t *testing.T) {
	p := pool(t)
	fake, _ := newFake(t)
	otherKey, _ := newKey(t)
	d := newDeliverer(t, p, fake, otherKey)
	seedWithFeedOn(t, p)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)

	_, err := d.pass(t)

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "got %v", err)
	assert.Empty(t, fake.received(), "the fake's verifier stopped it before the handler")
	assert.Len(t, outboxMarks(t, p), 1)
}

func TestPolicyDelivery_AFeedThatIsOffSendsNothing(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	execSQL(t, p, `INSERT INTO policy_outbox (entity_type, entity_id) VALUES ('Iam::Team', $1)`, outboxTeam)
	require.False(t, feedEnabled(t, p))

	result, err := d.pass(t)

	require.NoError(t, err)
	assert.Zero(t, result.Marks)
	assert.Empty(t, fake.received())
	assert.Len(t, outboxMarks(t, p), 1)

	// The control: the same mark goes once the feed is on.
	startFeed(t, p)
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Len(t, fake.received(), 1)
}

func TestPolicyDelivery_OneReplicaDeliversAtATime(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	seedWithFeedOn(t, p)
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, outboxTeam)

	// Another replica mid-pass holds the feed's row.
	holder, err := p.Begin(context.Background())
	require.NoError(t, err)
	_, err = holder.Exec(context.Background(), `SELECT enabled FROM policy_feed FOR UPDATE`)
	require.NoError(t, err)

	result, err := d.pass(t)
	require.NoError(t, err)
	assert.Zero(t, result.Marks, "a replica that cannot take the row does not wait for it")
	assert.Empty(t, fake.received())

	require.NoError(t, holder.Rollback(context.Background()))
	result, err = d.pass(t)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Marks)
}

func TestPolicyDelivery_ABatchHoldsAtMostTheLimit(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	seedWithFeedOn(t, p)
	execSQL(t, p, `INSERT INTO policy_outbox (entity_type, entity_id)
		SELECT 'Iam::Team', 'grn:acme/iam/group/g' || n FROM generate_series(1, 7) n`)

	result, err := d.store.Deliver(context.Background(), 3, d.sender)
	require.NoError(t, err)

	assert.Equal(t, 3, result.Marks)
	assert.Len(t, outboxMarks(t, p), 4)
	assert.Len(t, fake.received()[0].GetChanges(), 3)
}
