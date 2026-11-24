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
	"encoding/hex"
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

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/ca"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/user"
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
	code := m.Run()
	if cliDir != "" {
		_ = os.RemoveAll(cliDir)
	}
	os.Exit(code)
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
	// cluster_declaration_ids outlives every organization by design
	// (ADR-0187), so no cascade from orgs empties it.
	_, err := p.Exec(context.Background(), `TRUNCATE users, orgs, groups, projects,
		group_members, project_members, org_members, invitations, sessions,
		oauth_states, password_credentials, password_attempts,
		agents, agent_assignments, operators, certificate_authorities,
		operator_assertions,
		messages, message_consents, message_signing_keys, agent_watches,
		audit_send_events, policy_outbox,
		cluster_declaration_ids CASCADE`)
	require.NoError(t, err)
	// A suite that turned the feed on and died before it turned it off
	// would otherwise leave every later test writing marks.
	_, err = p.Exec(context.Background(), `UPDATE policy_feed SET enabled = false`)
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
// it, which the machine listener reads on every request to admit its
// token (ADR-0070).
//
// A GRN the org already holds is left as it is rather than refused.
// Registering is what standing an operator up takes, and more than one
// helper here does it for the same subject; a second call meaning
// "make sure this is registered" is what those callers want, and the
// refusal for a genuine second registration is asserted where that is
// the subject.
func registerOperator(t *testing.T, p *pgxpool.Pool, g grn.GRN) grn.GRN {
	t.Helper()
	store := operatorpg.New(p)
	if _, err := store.Get(context.Background(), g); err == nil {
		return g
	}
	o, err := operator.New(orgRootGRN(g.Organization), g.Identifier, scopedGRN(g.Organization, "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = store.Register(context.Background(), o)
	require.NoError(t, err)
	return o.GRN
}

func grnOf(serviceType, id string) grn.GRN {
	owner := "acme"
	if serviceType == "org" {
		owner = "root"
	}
	return grn.New(owner, serviceType, id)
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

// tokenless is what an agent with no activation presents to the
// execution fence: a credential inside its dates and no token, which
// the fence refuses for an agent it holds before it reads the
// credential (ADR-0118).
func tokenless(g grn.GRN) agent.Fencing {
	now := time.Now()
	return agent.Fencing{Agent: g, Credential: agent.Credential{Serial: "t" + strings.Repeat("0", 32),
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)}}
}

// activatedFence activates g for assignee through the store, replacing
// replaces where one is given, and answers the fence its writes then
// present: a token minted for g and the activation's token. It answers
// the activation too, for the next one to replace.
func activatedFence(t *testing.T, p *pgxpool.Pool, g, assignee grn.GRN, requestID string, gen int, replaces *agent.ActivationID) (agent.Fencing, agent.ActivationID) {
	t.Helper()
	withLineage(t, p, g)
	f := fence{p: p, agents: agentpg.New(p), exec: agentpg.New(p), agent: g, controller: assignee}
	c := f.valid(t, "1")
	a, token := f.activate(t, requestID, generation(gen), replaces, c)
	return fencing(g, &token, c), a.ID
}

// seedAgent mints an agent of the org and stores it.
func seedAgent(t *testing.T, s *agentpg.Store, orgGRN grn.GRN) grn.GRN {
	t.Helper()
	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(context.Background(), a))
	return g
}

func ptr[T any](v T) *T { return &v }

// newPolicyOrg creates the org a policy hangs off and returns its GRN.
func newPolicyOrg(t *testing.T, p *pgxpool.Pool) grn.GRN {
	t.Helper()
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgpg.New(p).CreateOrg(context.Background(), org.NewOrganization(orgGRN, "acme")))
	return orgGRN
}

// orgRootGRN returns the root-owned GRN of the named organization.
func orgRootGRN(name string) grn.GRN {
	return grn.New(grn.RootOwner, "org", name)
}

// scopedGRN names a resource owned by an arbitrary org, where grnOf is
// fixed to "acme".
func scopedGRN(orgName, serviceType, id string) grn.GRN {
	return grn.New(orgName, serviceType, id)
}

// briefDelivery is the queue's delivery configuration with the waits
// shortened to what a test can sit through. What each parameter governs
// is unchanged; `garam serve machine` runs on the defaults.
//
// Wait exceeds Lease by a second, which TestMessageStatus_ASender... relies
// on: it takes a message, lets the lease lapse and polls again, and that
// poll is the wait. A poll starts after the claim it waits out and its last
// query can start before the lease lapses, so a Wait equal to Lease leaves
// the poll only the milliseconds between them and it answers 204 under
// load (#1982). A second is far past what a loaded machine adds to one
// request.
func briefDelivery() message.Config {
	return message.Config{
		Lease:        2 * time.Second,
		LeaseCeiling: 5 * time.Second,
		Attempts:     2,
		Wait:         3 * time.Second,
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

// sameOrganizationSends is the policydecision.Service the default
// suite's machine listener asks about a send, in the place of the
// policy service this suite does not run (ADR-0168). It answers what
// the service answers for the pairs the suite sends between: allow
// where sender and recipient share an organization, deny across the
// boundary, where no consent relation is held. The suite tagged
// policyservice asks the real service instead.
type sameOrganizationSends struct{ policydecision.Service }

func (sameOrganizationSends) Evaluate(_ context.Context, q policydecision.Question) (policydecision.Decision, error) {
	return policydecision.Decision{Allowed: q.Subject.Organization == q.Resource.Organization}, nil
}

// sendsRefusing is sameOrganizationSends with the pairs a test names
// denied, which is what the service answers once an organization's
// policy denies the send (ADR-0168). Which policy denies it, and
// whom it attaches to, is the service's and is held by its own
// suites; this suite holds what the listener does with the answer.
type sendsRefusing struct {
	sameOrganizationSends
	mu     sync.Mutex
	denied map[[2]string]bool
}

// deny makes every send from sender to recipient a denial.
func (s *sendsRefusing) deny(sender, recipient grn.GRN) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denied == nil {
		s.denied = map[[2]string]bool{}
	}
	s.denied[[2]string{sender.String(), recipient.String()}] = true
}

func (s *sendsRefusing) Evaluate(ctx context.Context, q policydecision.Question) (policydecision.Decision, error) {
	s.mu.Lock()
	denied := s.denied[[2]string{q.Subject.String(), q.Resource.String()}]
	s.mu.Unlock()
	if denied {
		return policydecision.Decision{}, nil
	}
	return s.sameOrganizationSends.Evaluate(ctx, q)
}

// currentDecisions is the policydecision.Service the default suite's
// listeners and browser-facing authority ask about what a user or an
// operator may do now (ADR-0146 items 14 and 15), in the place of the
// policy service this suite does not run. A question of an agent that
// does not ask read-current is a send, answered as
// sameOrganizationSends answers it.
//
// The browser listener's enforcement asks it too (#1974): a question
// of a user, current or not, is a decision over the state the service
// has been delivered, since each test here writes its state through the
// platform and a writer waits for its marks to be delivered (ADR-0146
// item 5), and it is answered from the role the membership holds.
//
// It answers as the service does for the state the platform feeds it
// and nothing more: a user is allowed what the role the membership
// holds now carries, an operator is allowed what the platform has
// checked its grant covers, and either is denied where a test forbids
// the pair; a test can also make it fail or stall. What the service
// itself reads, the fed operations, the guardrails and the role links,
// is held by the suite tagged policyservice against the real service.
type currentDecisions struct {
	policydecision.Service
	orgs org.Store

	mu        sync.Mutex
	forbidden map[[2]string]bool
	forbidOns map[[3]string]bool
	permitted map[[2]string]bool
	failure   error
	held      chan struct{}
	asked     []policydecision.Question
	enforced  []policydecision.Question
}

func newCurrentDecisions(p *pgxpool.Pool) *currentDecisions {
	return &currentDecisions{Service: sameOrganizationSends{}, orgs: orgpg.New(p)}
}

// forbid makes every question of subject about action a denial.
func (c *currentDecisions) forbid(subject grn.GRN, action string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.forbidden == nil {
		c.forbidden = map[[2]string]bool{}
	}
	c.forbidden[[2]string{subject.String(), action}] = true
}

// permit makes every question of subject about action allowed that no
// forbid refuses: what a permit the owner wrote adds to the role.
func (c *currentDecisions) permit(subject grn.GRN, action string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.permitted == nil {
		c.permitted = map[[2]string]bool{}
	}
	c.permitted[[2]string{subject.String(), action}] = true
}

// forbidOn makes every question of subject about action on resource a
// denial: the one pair a guardrail would forbid, where forbid forbids
// the action everywhere.
func (c *currentDecisions) forbidOn(subject grn.GRN, action string, resource grn.GRN) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.forbidOns == nil {
		c.forbidOns = map[[3]string]bool{}
	}
	c.forbidOns[[3]string{subject.String(), action, resource.String()}] = true
}

// failWith makes every question of a user, and every read-current one,
// fail with err.
func (c *currentDecisions) failWith(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failure = err
}

// stall makes every question of a user, and every read-current one,
// wait, answering nothing, until the function it returns is called or
// the caller gives up.
func (c *currentDecisions) stall() func() {
	held := make(chan struct{})
	c.mu.Lock()
	c.held = held
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.held = nil
			c.mu.Unlock()
			close(held)
		})
	}
}

// currentQuestions are the read-current questions asked so far.
func (c *currentDecisions) currentQuestions() []policydecision.Question {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]policydecision.Question(nil), c.asked...)
}

// questions are every question it decided rather than answered as a
// send: the read-current ones, and those the browser listener's
// enforcement, its permission checks, its holders list and the probes
// of a list or the search ask of a user.
func (c *currentDecisions) questions() []policydecision.Question {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]policydecision.Question(nil), c.enforced...)
}

// Evaluations answers each question as Evaluate does, and fails whole
// on the first that errs, as the service's client does.
func (c *currentDecisions) Evaluations(ctx context.Context, qs []policydecision.Question) ([]policydecision.Decision, error) {
	out := make([]policydecision.Decision, len(qs))
	for i, q := range qs {
		d, err := c.Evaluate(ctx, q)
		if err != nil {
			return nil, err
		}
		out[i] = d
	}
	return out, nil
}

func (c *currentDecisions) Evaluate(ctx context.Context, q policydecision.Question) (policydecision.Decision, error) {
	c.mu.Lock()
	key := [2]string{q.Subject.String(), q.Action}
	permitted := c.permitted[key]
	failure := c.failure
	forbidden := c.forbidden[key] ||
		c.forbidOns[[3]string{q.Subject.String(), q.Action, q.Resource.String()}]
	held := c.held
	// A question no control names, from an agent, is a send's and is
	// answered as it is without them. A control a test wrote is read
	// before that, so it is never ignored.
	delegated := !q.Current && q.Subject.ServiceType != user.ServiceType &&
		!permitted && !forbidden && failure == nil && held == nil
	if !delegated {
		if q.Current {
			c.asked = append(c.asked, q)
		}
		c.enforced = append(c.enforced, q)
	}
	c.mu.Unlock()
	if delegated {
		return c.Service.Evaluate(ctx, q)
	}
	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return policydecision.Decision{}, ctx.Err()
		}
	}
	switch {
	case failure != nil:
		return policydecision.Decision{}, failure
	case forbidden:
		return policydecision.Decision{}, nil
	case q.Subject.ServiceType != user.ServiceType:
		return policydecision.Decision{Allowed: true}, nil
	case permitted:
		return policydecision.Decision{Allowed: true}, nil
	}
	owning := q.Resource.Organization
	if q.Resource.ServiceType == "org" {
		owning = q.Resource.Identifier
	}
	m, err := c.orgs.GetMember(ctx, grn.Org(owning), q.Subject)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return policydecision.Decision{}, nil
	}
	if err != nil {
		return policydecision.Decision{}, err
	}
	return policydecision.Decision{Allowed: matchAny(m.Role.Actions(), q.Action) &&
		matchAny([]string{grn.Org(owning).String(), grn.OrgPattern(owning)}, q.Resource.String())}, nil
}

// matchAny reports whether value matches one of patterns, where `*`
// matches any run of characters and `?` any one.
func matchAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, value) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, value string) bool {
	px, vx := 0, 0
	star, starV := -1, 0
	for vx < len(value) {
		switch {
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == value[vx]):
			px++
			vx++
		case px < len(pattern) && pattern[px] == '*':
			star = px
			starV = vx
			px++
		case star != -1:
			px = star + 1
			starV++
			vx = starV
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

// refusalText is the body of the refusal garam stores to a sender whose
// message to recipient the policy service did not allow across the
// boundary, or whose recipient is no agent (ADR-0168). It names neither
// consent nor absence, so the two read alike.
func refusalText(recipient grn.GRN) string {
	return "garam could not deliver your message to " + recipient.String() +
		": a message to this recipient is not permitted"
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
// reads from its configuration is supplied here instead: the sealer the
// caller holds, a self-signed listener certificate, and delivery waits
// shortened to what a test can sit through.
func machineListener(t *testing.T, p *pgxpool.Pool, sealer ca.Sealer) string {
	t.Helper()
	return machineListenerHolding(t, p, sealer, nil)
}

// machineListenerHolding is machineListener with its message signing
// keys created in signing, or sealed under sealer where signing is nil
// (ADR-0104).
func machineListenerHolding(t *testing.T, p *pgxpool.Pool, sealer ca.Sealer, signing message.Custodian) string {
	t.Helper()
	return machineListenerDeciding(t, p, sealer, signing, newCurrentDecisions(p))
}

// machineListenerDeciding is machineListenerHolding with the sends it
// serves decided by decisions: the double the default suite uses, or
// the real policy service where the suite runs against one.
func machineListenerDeciding(t *testing.T, p *pgxpool.Pool, sealer ca.Sealer, signing message.Custodian, decisions policydecision.Service) string {
	t.Helper()
	presented := loopbackCertificate(t)
	srv, err := cli.NewMachineServer(p, cli.MachineConfig{
		Sealer:           sealer,
		SigningCustodian: signing,
		Issuer:           testIssuer,
		Certificate:      presented,
		ServerRoot:       certificatePEM(presented),
		Delivery:         briefDelivery(),
		Decisions:        decisions,
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

// tokenPresenting reaches the machine listener presenting token on
// every request that names no Authorization of its own.
func tokenPresenting(t *testing.T, token string) *http.Client {
	t.Helper()
	client, _ := tokenClient(t)
	client.Transport = withHeaders{base: client.Transport, headers: map[string]string{"Authorization": "Garam-Token " + token}}
	return client
}

// openedLineage gives g's execution a lineage of its own and no
// activation, as a managed creation opens one: an agent outside a
// lineage is minted no token (ADR-0100).
func openedLineage(t *testing.T, p *pgxpool.Pool, g grn.GRN) {
	t.Helper()
	require.EqualValues(t, 1, withLineage(t, p, g), "the agent held no lineage")
}

// withLineage is openedLineage for an agent that may already hold one,
// which it leaves as it is. It answers how many it opened.
func withLineage(t *testing.T, p *pgxpool.Pool, g grn.GRN) int64 {
	t.Helper()
	tag, err := p.Exec(context.Background(),
		`UPDATE agent_executions SET lineage = gen_random_uuid(), updated_at = now() WHERE agent_grn = $1 AND lineage IS NULL`, g.String())
	require.NoError(t, err)
	return tag.RowsAffected()
}

// operatorClient registers subject where its organization holds no
// record of it, issues it a key, exchanges an assertion signed by that
// key for garam's token on the listener at url, and answers a client
// presenting the token on every request, as an operator's controller
// does (ADR-0102, ADR-0118).
func operatorClient(t *testing.T, p *pgxpool.Pool, url string, subject grn.GRN) *http.Client {
	t.Helper()
	registerOperator(t, p, subject)
	private, _ := issueKey(t, operatorpg.New(p), subject, operator.KeyRotationFirst)
	client, _ := tokenClient(t)
	return tokenPresenting(t, exchangeKey(t, client, url, subject, private, nil))
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

// provisioned stands an agent up for the calling operator and returns
// its GRN with a client that authenticates as it. The agent is
// assigned to the operator, activated once, recovered into a lineage
// and activated in it, and holds a token the operator minted there, so
// the client presents that token and the activation's on every
// request, as a fenced agent's adapter does (ADR-0100, ADR-0118). Its
// lifecycle is written through the stores rather than through a
// lifecycle authority, which the execution suites exercise; the
// operator is read off the listener, as the identity it authenticates.
func provisioned(t *testing.T, url string, operator *http.Client) (grn.GRN, *http.Client) {
	t.Helper()
	status, body := machineRequest(t, operator, http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusOK, status, string(body))
	var identity machinegen.Identity
	require.NoError(t, json.Unmarshal(body, &identity))
	operatorGRN, err := grn.Parse(identity.Grn)
	require.NoError(t, err)

	f, _, execution := lineaged(t, connect(t, os.Getenv("GARAM_TEST_DATABASE_URL"), nil), operatorGRN)
	return f.agent, f.client(t, execution)
}

// client is a machine client for f's agent: it presents a token minted
// for it now beside execution, the activation's token, as the agent's
// adapter does on every call.
func (f fence) client(t *testing.T, execution agent.Token) *http.Client {
	t.Helper()
	token := f.mintedIn(t)
	client, _ := tokenClient(t)
	client.Transport = withHeaders{base: client.Transport, headers: map[string]string{
		"Authorization": "Garam-Token " + token.Value, "Garam-Execution": execution.String(),
	}}
	return client
}

// lineaged stands an agent up for operator through the stores:
// assigned to it, activated once, recovered into a lineage and
// activated in it, which is where an agent's tokens are minted. It
// answers the agent's fence and the current activation with its token.
func lineaged(t *testing.T, p *pgxpool.Pool, operator grn.GRN) (fence, *agent.Activation, agent.Token) {
	t.Helper()
	agents := agentpg.New(p)
	g := seedAgent(t, agents, grn.Org(operator.Organization))
	_, err := agents.Assign(context.Background(), g, operator)
	require.NoError(t, err)
	openedLineage(t, p, g)
	f := fence{p: p, agents: agents, exec: agentpg.New(p), agent: g, controller: operator}
	ended := f.openLineage(t)
	current, token := f.activate(t, "lineaged", generation(1), &ended, f.valid(t, "1"))
	return f, current, token
}

// withHeaders is a transport that sets each of its headers on a request
// that carries none of that name, as an agent's adapter sends its own
// credentials beside whatever a call adds.
type withHeaders struct {
	base    http.RoundTripper
	headers map[string]string
}

func (p withHeaders) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for name, value := range p.headers {
		if r.Header.Get(name) == "" {
			r.Header.Set(name, value)
		}
	}
	return p.base.RoundTrip(r)
}

// anonymousMachineClient reaches the listener presenting no credential
// at all.
func anonymousMachineClient(t *testing.T) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// keyEncryptionKey writes the key-encryption key the message signing
// keys are sealed under the way ADR-0015 has key material arrive — a
// file its owner alone can read — and returns the path beside the
// sealer that opens it. A command under test reads the file; the
// listener beside it takes the sealer.
func keyEncryptionKey(t *testing.T) (string, ca.Sealer) {
	t.Helper()
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.key")
	require.NoError(t, os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600))
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	return path, sealer
}

// register records an operator of acme and answers its GRN.
func register(t *testing.T, s *operatorpg.Store, id string) grn.GRN {
	t.Helper()
	o, err := operator.New(orgRootGRN("acme"), id, scopedGRN("acme", "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.Register(context.Background(), o)
	require.NoError(t, err)
	return o.GRN
}

// stateOf reads one operator out of the organization's listing, which
// is the read the console makes.
func stateOf(t *testing.T, s *operatorpg.Store, g grn.GRN) *operator.Operator {
	t.Helper()
	listed, err := s.ListByOrg(context.Background(), orgRootGRN("acme"))
	require.NoError(t, err)
	for _, o := range listed {
		if o.GRN == g {
			return o
		}
	}
	t.Fatalf("operator %s is not in the listing", g)
	return nil
}
