// Package integration_test exercises garam's modules in process
// against a real database: the Postgres stores, the services layered
// over them and the listeners that serve them. GARAM_TEST_DATABASE_URL
// names that database; `make db-up integration` starts the
// docker-compose Postgres and sets it. The package fails without it
// rather than skipping.
package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

var migrateOnce sync.Once

// migrationsDir returns the repo-root `migrations/` directory. It walks
// up from this source file until it finds go.mod, so the suite keeps
// working wherever the test directory is moved to.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "cannot locate the test source file")
	for dir := filepath.Dir(file); ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations")
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above %s", file)
		dir = parent
	}
}

// TestMain requires the test database before any test runs. The suite
// is written against Postgres rather than a substitute, and skipping
// instead left `go test` printing the same `ok` for a run that touched
// no database as for one that passed against it.
func TestMain(m *testing.M) {
	if os.Getenv("GARAM_TEST_DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "integration: GARAM_TEST_DATABASE_URL is not set; run `make db-up integration`")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// pool connects to the test database TestMain checked for.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("GARAM_TEST_DATABASE_URL")
	migrateOnce.Do(func() {
		m, err := migrate.New("file://"+migrationsDir(t), url)
		require.NoError(t, err)
		err = m.Up()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			require.NoError(t, err)
		}
	})
	p := connect(t, url, nil)
	truncate(t, p)
	return p
}

// poolConns is every suite pool's MaxConns. pgxpool's default is
// max(4, NumCPU), so a test holding more sessions than a small runner
// has cores waited on its own sessions there and passed elsewhere
// (#1518). The widest test holds ten at once.
const poolConns = 16

// connect opens a pool of poolConns connections on url, after tune
// adjusts its configuration when given, and closes it with the test.
func connect(t *testing.T, url string, tune func(*pgxpool.Config)) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	cfg.MaxConns = poolConns
	if tune != nil {
		tune(cfg)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}

// truncate resets every table so tests are independent.
func truncate(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	_, err := p.Exec(context.Background(), `TRUNCATE users, orgs, groups, projects,
		group_members, project_members, org_members, invitations, sessions,
		oauth_states, password_credentials, password_attempts,
		policies, policy_versions, policy_attachments, agents,
		agent_assignments, agent_definitions, operators, certificate_authorities,
		operator_certificates, operator_assertions,
		messages, message_consents, message_signing_keys, agent_watches,
		audit_send_events CASCADE`)
	require.NoError(t, err)
}

// countRows answers a count query against the test database. A store
// that removes a row is asserted on the table: a getter that filters by
// expiry answers the same for a row removed and a row ignored.
func countRows(t *testing.T, p *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, p.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

// registerOperator records that the org owning g has an operator under
// it, which a definition naming one requires (ADR-0045) and which the
// machine listener reads per connection to admit its certificate
// (ADR-0070).
//
// A GRN the org already holds is left as it is rather than refused.
// Registering is what standing an operator up takes, and more than one
// helper here does it for the same subject; a second call meaning
// "make sure this is registered" is what those callers want, and the
// refusal for a genuine second registration is asserted where that is
// the subject.
func registerOperator(t *testing.T, p *pgxpool.Pool, g grn.GRN) grn.GRN {
	t.Helper()
	store := operator.NewPostgresStore(p)
	if _, err := store.Get(context.Background(), g); err == nil {
		return g
	}
	o, err := operator.New(orgRootGRN(g.Organization), g.Identifier, scopedGRN(g.Organization, "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = store.Register(context.Background(), o)
	require.NoError(t, err)
	return o.GRN()
}

func grnOf(serviceType, id string) grn.GRN {
	owner := "acme"
	if serviceType == "org" {
		owner = "root"
	}
	return grn.New(owner, "default", serviceType, id)
}

// stamps is the created/updated pair an aggregate carrying both
// surrenders. It is a value the caller reads off the aggregate rather
// than an interface over it: an aggregate surrenders the pair as
// exported fields (ADR-0069) or as accessors, and a field satisfies no
// method set, so only a value serves both shapes while the domains
// convert. The assertions below read the same whichever aggregate
// holds them, so they are written once.
type stamps struct {
	created time.Time
	updated time.Time
}

// assertInserted holds what a row a store has just written carries: a
// created_at inside the window the insert ran in, and an updated_at
// equal to it, because the constructor stamps both from one instant.
// Postgres keeps microseconds and Go keeps nanoseconds, so the lower
// bound is truncated to what the column can hold.
func assertInserted(t *testing.T, a stamps, before, after time.Time) {
	t.Helper()
	assert.WithinRange(t, a.created, before.Truncate(time.Microsecond), after)
	assert.True(t, a.updated.Equal(a.created),
		"a row nothing has updated carries one instant in both columns")
}

// assertUpdated holds what an update leaves: created_at where the
// insert put it, and updated_at moved past it. A field or an accessor
// answering with the zero time, or with the other of the two, fails
// one half or the other here rather than passing quietly.
func assertUpdated(t *testing.T, was, now stamps) {
	t.Helper()
	assert.True(t, now.created.Equal(was.created),
		"an update leaves created_at where the insert put it")
	assert.True(t, now.updated.After(was.updated),
		"an update moves updated_at past what it was")
}

// legacyFence is what an agent never activated presents to the
// execution fence: a valid certificate and no token.
func legacyFence(g grn.GRN) agent.Fencing {
	return agent.Fencing{Agent: g, Credential: valid("1")}
}

// seedAgent mints an agent of the org and stores it.
func seedAgent(t *testing.T, s *agent.PostgresStore, orgGRN grn.GRN) grn.GRN {
	t.Helper()
	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(context.Background(), a))
	return g
}

func ptr[T any](v T) *T { return &v }

// policyDoc builds a one-statement document whose Sid identifies the
// version it belongs to.
func policyDoc(sid string, effect wire.Effect) authz.PolicyDocument {
	return statementDoc(sid, effect, "org:read", "grn:acme:*")
}

// statementDoc builds a one-statement document over an explicit action
// and resource pattern.
func statementDoc(sid string, effect wire.Effect, action, resource string) authz.PolicyDocument {
	return authz.PolicyDocument{
		Version: "2026-07-25",
		Statement: []authz.Statement{{
			Sid:      sid,
			Effect:   effect,
			Action:   []string{action},
			Resource: []string{resource},
		}},
	}
}

// newPolicyOrg creates the org a policy hangs off and returns its GRN.
func newPolicyOrg(t *testing.T, p *pgxpool.Pool) grn.GRN {
	t.Helper()
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgpg.New(p).CreateOrg(context.Background(), org.NewOrganization(orgGRN, "acme")))
	return orgGRN
}

// orgRootGRN returns the root-owned GRN of the named organization.
func orgRootGRN(name string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", name)
}

// scopedGRN names a resource owned by an arbitrary org, where grnOf is
// fixed to "acme".
func scopedGRN(orgName, serviceType, id string) grn.GRN {
	return grn.New(orgName, grn.DefaultProject, serviceType, id)
}

// activePolicy creates and activates a policy in orgGRN through the
// lifecycle service, so the cache is dropped the way a production write
// drops it. It returns the policy GRN in wire form.
func activePolicy(t *testing.T, srv *authz.PolicyGRPCServer, orgGRN grn.GRN, name string, doc authz.PolicyDocument) string {
	t.Helper()
	ctx := context.Background()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      name,
		Document:  string(raw),
		CreatedBy: scopedGRN(orgGRN.Identifier, "user", "ada").String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	return policyGRN
}

// attachPolicy attaches policyGRN to subject through the lifecycle
// service.
func attachPolicy(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string, subject grn.GRN) {
	t.Helper()
	_, err := srv.AttachPolicy(context.Background(), &authzv1.AttachPolicyRequest{
		PolicyGrn:  policyGRN,
		SubjectGrn: subject.String(),
	})
	require.NoError(t, err)
}

// briefDelivery is the queue's delivery configuration with the waits
// shortened to what a test can sit through. What each parameter governs
// is unchanged; `garam serve machine` runs on the defaults.
func briefDelivery() message.Config {
	return message.Config{
		Lease:        2 * time.Second,
		LeaseCeiling: 5 * time.Second,
		Attempts:     2,
		Wait:         2 * time.Second,
		PollInterval: 20 * time.Millisecond,
	}
}

// listenerCertificate is what garam presents, as the listener asks for
// it (machine.ServerCertificate).
type listenerCertificate tls.Certificate

// loopbackCertificate is what the listener terminates TLS with. What
// garam presents is not what this test is about, so it is self-signed
// and the client below does not verify it.
func loopbackCertificate(t *testing.T) listenerCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "garam machine listener"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return listenerCertificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// Get answers every handshake with the one certificate above. The
// listener asks per handshake so a replaced certificate is served
// without a restart (ADR-0040); nothing replaces this one.
func (c listenerCertificate) Get() (*tls.Certificate, error) {
	certificate := tls.Certificate(c)
	return &certificate, nil
}

// certificatePEM renders what the listener presents in the form an
// agent receives the garam server root in (ADR-0040). Nothing here
// signs the listener's certificate, so the root it chains to is itself.
func certificatePEM(c listenerCertificate) []byte {
	var out []byte
	for _, der := range c.Certificate {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	return out
}

// policyEngine serves the authz engine over gRPC against the policies
// Postgres holds, and returns the lifecycle service an organization
// writes them through beside the seam the machine listener asks
// through. It is the pair of processes `garam serve authz` and `garam
// serve machine` are, over one database.
func policyEngine(t *testing.T, p *pgxpool.Pool) (*authz.PolicyGRPCServer, *grpc.ClientConn) {
	t.Helper()
	store := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(store)

	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(cache))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	// Writes made through this service drop the org's cache entry
	// directly, as they do in the engine's own process.
	return authz.NewPolicyGRPCServer(store, cache), conn
}

// testIssuer is the issuer every listener here signs messages as
// (ADR-0101). No verifier dials it: a test reads the key set from the
// listener's own address.
const testIssuer message.Issuer = "https://garam.test"

// testTokenURL is the token endpoint every listener here is configured
// with, which an operator's assertion names as its audience (ADR-0102).
// It is not the listener's address: the audience is garam's configured
// value, not the one a caller dials.
const testTokenURL = "https://garam.test/oauth/token"

// machineListener serves the server `garam serve machine` builds,
// over the stores Postgres holds, and returns its URL. What the command
// reads from its configuration is supplied here instead: the sealer and
// engine the caller holds, a self-signed listener certificate, and
// delivery waits shortened to what a test can sit through.
func machineListener(t *testing.T, p *pgxpool.Pool, sealer ca.Sealer, engine grpc.ClientConnInterface) string {
	t.Helper()
	return machineListenerHolding(t, p, sealer, engine, nil)
}

// machineListenerHolding is machineListener with its message signing
// keys created in signing, or sealed under sealer where signing is nil
// (ADR-0104).
func machineListenerHolding(t *testing.T, p *pgxpool.Pool, sealer ca.Sealer, engine grpc.ClientConnInterface, signing message.Custodian) string {
	t.Helper()
	presented := loopbackCertificate(t)
	srv, err := cli.NewMachineServer(p, cli.MachineConfig{
		Sealer:           sealer,
		SigningCustodian: signing,
		Issuer:           testIssuer,
		Certificate:      presented,
		ServerRoot:       certificatePEM(presented),
		Delivery:         briefDelivery(),
		Engine:           engine,
		Logger:           slog.New(slog.NewJSONHandler(io.Discard, nil)),
		TokenURL:         testTokenURL,
	})
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return "https://" + lis.Addr().String()
}

// installed returns the certificate garam signs for subject as the
// process holding it presents one. An operator's own first certificate
// arrives out of band (ADR-0015); an agent's comes from the surface
// under test.
//
// An operator is registered here as well, because a certificate is
// half of what one needs: the machine listener reads the organization's
// record per connection, so an operator nobody registered does not
// authenticate however it obtained its certificate (ADR-0070). An
// agent has no such record and is left alone.
func installed(t *testing.T, p *pgxpool.Pool, issuer ca.Service, subject grn.GRN) tls.Certificate {
	t.Helper()
	if subject.ServiceType == operator.ServiceType {
		registerOperator(t, p, subject)
	}
	issued, err := issuer.Issue(context.Background(), subject)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)
	return pair
}

// machineClient presents pair on every request it makes, the way a
// process holding one certificate reaches this listener.
func machineClient(t *testing.T, pair tls.Certificate) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{pair},
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// machineRequest performs one request and returns the status and body.
func machineRequest(t *testing.T, client *http.Client, method, url string) (int, []byte) {
	t.Helper()
	return machineSend(t, client, method, url, "")
}

// machineSend performs one request carrying body and returns the status
// and what came back.
func machineSend(t *testing.T, client *http.Client, method, url, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, answer
}

// submissionTo is the body a sender posts to address recipient.
func submissionTo(recipient grn.GRN, key string) string {
	return `{"recipient":"` + recipient.String() + `","idempotencyKey":"` + key + `","body":"hello"}`
}

// pairOf is the certificate the surface issued, as the process it was
// issued for presents it.
func pairOf(t *testing.T, issued machinegen.MachineCertificate) tls.Certificate {
	t.Helper()
	pair, err := tls.X509KeyPair([]byte(issued.CertificatePem), []byte(issued.PrivateKeyPem))
	require.NoError(t, err)
	return pair
}

// provisioned creates an agent through the surface as the calling
// operator and returns its GRN with a client presenting the certificate
// the surface issued for it. It is the two calls
// TestMachineSurface_OperatorCreatesAnAgentThatThenAuthenticates covers
// in full, as the tests that need an authenticated agent make them.
func provisioned(t *testing.T, url string, operator *http.Client) (grn.GRN, *http.Client) {
	t.Helper()
	status, body := machineRequest(t, operator, http.MethodPost, url+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &created))

	status, body = machineRequest(t, operator, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	require.Equal(t, http.StatusCreated, status, string(body))
	var issued machinegen.MachineCertificate
	require.NoError(t, json.Unmarshal(body, &issued))

	agentGRN, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	return agentGRN, machineClient(t, pairOf(t, issued))
}

// enrollmentRequest generates a key pair the way an operator does in
// its own cluster and returns the certificate signing request over it
// with the key that stays there.
func enrollmentRequest(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})), key
}

// anonymousMachineClient reaches the listener presenting no certificate
// at all, which is what an operator that has never held one has.
func anonymousMachineClient(t *testing.T) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}
