//go:build policymatrix

package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/ca"
	"github.com/garamsh/garam/platform/internal/cli"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
	decisiongrpc "github.com/garamsh/garam/platform/internal/policydecision/grpc"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	feedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
	feedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
	"github.com/garamsh/garam/platform/internal/signer"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// The matrix asks the platform and the stack's policy service the same
// questions over the same state and lists where they answer
// differently (#1915). It reads the stack through the variables
// `make -C platform policy-matrix` sets.
const (
	matrixAddrEnv   = "POLICY_MATRIX_ADDR"
	matrixCAEnv     = "POLICY_MATRIX_CA"
	matrixKeyEnv    = "POLICY_MATRIX_KEY"
	matrixIssuerEnv = "POLICY_MATRIX_ISSUER"
	// matrixReportEnv names a file the whole table is written to.
	matrixReportEnv = "POLICY_MATRIX_REPORT"
	// matrixAuthority is the name the stack's certificate holds; the
	// address is the container's, which the certificate does not name.
	matrixAuthority = "policy"
	// matrixBatch is below the 1000 evaluations one request may hold.
	matrixBatch = 500
	// matrixDifferences lists, one key per line, the questions the two
	// engines are known to answer differently.
	matrixDifferences = "testdata/policy_matrix_differences.txt"
	// pageSize is the most pairs one permission check takes.
	pageSize = 50
)

// answer is what one engine says to one question.
type answer string

const (
	answerAllow answer = "allow"
	answerDeny  answer = "deny"
	// answerUnknown is the platform's: its check allows and a
	// delegation grant it does not read decides the rest.
	answerUnknown answer = "unknown"
)

// how names the path by which the platform decided a row.
type how string

const (
	howRoute   how = "route"
	howRefused how = "route refused a non-member"
	howGrant   how = "delegation grant"
	howNoRoute how = "no route"
	howMachine how = "machine send"
)

// row is one question and the two engines' answers.
type row struct {
	key      string
	question policydecision.Question
	platform answer
	via      how
	service  policydecision.Decision
}

// agrees reads the platform's unknown as its check's allow.
func (r row) agrees() bool {
	return (r.platform != answerDeny) == r.service.Allowed
}

// refused reports that the service refused the question itself: the
// schema does not admit that subject, action and resource together.
func (r row) refused() bool {
	for x := range r.service.Reasons {
		if x.PolicyID == "" && x.Error != "" {
			return true
		}
	}
	return false
}

func (r row) serviceAnswer() answer {
	if r.service.Allowed {
		return answerAllow
	}
	return answerDeny
}

func (r row) reasons() []string {
	var out []string
	for x := range r.service.Reasons {
		out = append(out, x.PolicyID+"|"+x.Error)
	}
	slices.Sort(out)
	return out
}

type subject struct {
	label  string
	org    string
	grn    grn.GRN
	kind   string
	cookie *http.Cookie
}

type resource struct {
	label string
	org   string
	grn   grn.GRN
	kind  apigen.PermissionResourceType
}

// matrix is the state both engines hold.
type matrix struct {
	slug       map[string]string
	subjects   []subject
	resources  []resource
	browser    string
	machine    string
	grant      *operator.Grant
	controller grn.GRN
	hosted     grn.GRN
	senders    map[string]*http.Client
	agents     map[string]grn.GRN
	operators  map[string]*http.Client
	actions    []string
}

// TestPolicyMatrix asks every subject every action the platform
// enforces on every kind of resource the action admits, in the
// subject's organization and across it, of the platform and of the
// policy service. It fails on a difference the list does not hold and
// on a listed difference that is gone.
func TestPolicyMatrix(t *testing.T) {
	ctx := context.Background()
	addr, caFile, keyFile, issuer := matrixEnv(t)
	p := pool(t)
	startFeed(t, p)

	key, err := signer.LoadKey(keyFile)
	require.NoError(t, err)
	conn, err := feedgrpc.Dial(addr, caFile, issuer, key, grpc.WithAuthority(matrixAuthority))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := policyv1.NewPolicyServiceClient(conn)
	service := decisiongrpc.New(client)

	m := seedMatrix(t, p)
	deliverAll(t, p, feedpg.NewStore(p), feedgrpc.NewSender(client))

	rows := m.askPlatform(t)
	m.askService(t, service, rows)

	found := map[string]row{}
	agreeing, refused := 0, 0
	for _, r := range rows {
		if r.refused() {
			refused++
		}
		if r.agrees() {
			agreeing++
		} else {
			found[r.key] = r
		}
	}
	t.Logf("asked %d, agreeing %d, differing %d, refused by the service %d", len(rows), agreeing, len(found), refused)
	writeMatrixReport(t, rows)

	probeAgrees(t, ctx, service, rows)
	m.assertNoNonAgentSender(t)

	fresh, gone := compareDifferences(differenceKeys(found), listedDifferences(t))
	for _, k := range fresh {
		r := found[k]
		t.Errorf("new difference: %s platform=%s service=%s reasons=%v", k, r.platform, r.serviceAnswer(), r.reasons())
	}
	for _, k := range gone {
		t.Errorf("listed difference is gone: %s", k)
	}
}

// TestCompareDifferences_NamesWhatIsNewAndWhatIsGone holds the rule the
// matrix fails by: a difference the list lacks and a listed one that no
// longer appears are each named, and an equal pair names neither.
func TestCompareDifferences_NamesWhatIsNewAndWhatIsGone(t *testing.T) {
	fresh, gone := compareDifferences([]string{"a", "b"}, []string{"b", "c"})
	assert.Equal(t, []string{"a"}, fresh)
	assert.Equal(t, []string{"c"}, gone)

	fresh, gone = compareDifferences([]string{"a", "b"}, []string{"b", "a"})
	assert.Empty(t, fresh)
	assert.Empty(t, gone)
}

// compareDifferences answers the keys found and not listed, and the
// keys listed and not found.
func compareDifferences(found, listed []string) (fresh, gone []string) {
	for _, k := range found {
		if !slices.Contains(listed, k) {
			fresh = append(fresh, k)
		}
	}
	for _, k := range listed {
		if !slices.Contains(found, k) {
			gone = append(gone, k)
		}
	}
	slices.Sort(fresh)
	slices.Sort(gone)
	return fresh, gone
}

func differenceKeys(rows map[string]row) []string {
	out := make([]string, 0, len(rows))
	for k := range rows {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func listedDifferences(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(matrixDifferences)
	require.NoError(t, err)
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	require.NoError(t, sc.Err())
	return out
}

func matrixEnv(t *testing.T) (addr, caFile, keyFile, issuer string) {
	t.Helper()
	get := func(name string) string {
		v := os.Getenv(name)
		require.NotEmpty(t, v, "%s names the stack's policy service; run make -C platform policy-matrix", name)
		return v
	}
	return get(matrixAddrEnv), get(matrixCAEnv), get(matrixKeyEnv), get(matrixIssuerEnv)
}

// writeMatrixReport writes one line per question where the run asks
// for it, from which the document under docs/architecture is built.
func writeMatrixReport(t *testing.T, rows []row) {
	t.Helper()
	path := os.Getenv(matrixReportEnv)
	if path == "" {
		return
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n", r.key, r.platform, r.serviceAnswer(), r.via,
			strings.Join(r.reasons(), ";"))
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 3)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

// seedMatrix writes the state through the platform's own stores and
// listeners: three organizations, the first holding every kind of
// subject, the second consenting to it and the third not.
func seedMatrix(t *testing.T, p *pgxpool.Pool) *matrix {
	t.Helper()
	ctx := context.Background()
	sfx := randomSuffix(t)
	m := &matrix{
		slug:      map[string]string{"A": "mxa" + sfx, "B": "mxb" + sfx, "C": "mxc" + sfx},
		agents:    map[string]grn.GRN{},
		senders:   map[string]*http.Client{},
		operators: map[string]*http.Client{},
	}
	orgs, users, agents := orgpg.New(p), userpg.New(p), agentpg.New(p)
	orgGRN := func(label string) grn.GRN { return orgRootGRN(m.slug[label]) }
	for _, label := range []string{"A", "B", "C"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgGRN(label), m.slug[label])))
	}

	sessions := authnpg.NewSessionStore(p)
	member := func(label, orgLabel, id string, role org.Role) grn.GRN {
		g := grn.New(grn.RootOwner, user.ServiceType, id+sfx)
		require.NoError(t, users.Create(ctx, user.New(g, id+sfx)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(orgGRN(orgLabel), g, role)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, sessions.Create(ctx, token, authn.NewSession(g, time.Hour)))
		m.subjects = append(m.subjects, subject{label: label, org: orgLabel, grn: g, kind: "user",
			cookie: &http.Cookie{Name: "garam_session", Value: token}})
		return g
	}
	owner := member("A.owner", "A", "own", org.RoleOwner)
	member("A.admin", "A", "adm", org.RoleAdmin)
	member("A.member", "A", "mem", org.RoleMember)
	groupie := member("A.group-admin", "A", "grp", org.RoleMember)
	projectie := member("A.project-member", "A", "prj", org.RoleMember)
	member("B.member", "B", "bmem", org.RoleMember)
	member("C.member", "C", "cmem", org.RoleMember)

	for _, label := range []string{"A", "B", "C"} {
		var groupRoles, projectRoles []string
		if label == "A" {
			groupRoles, projectRoles = []string{string(org.RoleAdmin)}, []string{string(org.RoleMember)}
		}
		g := grn.New(m.slug[label], org.ServiceTypeGroup, "g1")
		require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(g, "g1", orgGRN(label), groupRoles...)))
		pr := grn.New(m.slug[label], org.ServiceTypeProject, "p1")
		require.NoError(t, orgs.CreateProject(ctx, org.NewProject(pr, "p1", orgGRN(label), projectRoles...)))
		if label == "A" {
			require.NoError(t, orgs.AddGroupMember(ctx, g, groupie))
			require.NoError(t, orgs.AddProjectMember(ctx, pr, projectie))
		}
	}

	consents := messagepg.New(p)
	_, err := consents.Grant(ctx, orgGRN("A"), orgGRN("B"))
	require.NoError(t, err)
	_, err = consents.Grant(ctx, orgGRN("B"), orgGRN("A"))
	require.NoError(t, err)

	key := make([]byte, ca.KeySize)
	_, err = rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	policies, engine := policyEngine(t, p)
	m.machine = machineListener(t, p, sealer, engine)
	for _, label := range []string{"A", "B", "C"} {
		op := operatorClient(t, p, m.machine, scopedGRN(m.slug[label], agent.ServiceTypeOperator, "op"))
		m.operators[label] = op
		g, client := provisioned(t, m.machine, op)
		m.agents[label], m.senders[label] = g, client
		m.subjects = append(m.subjects, subject{label: label + ".agent", org: label, grn: g, kind: "agent"})
	}
	g2, _ := provisioned(t, m.machine, m.operators["A"])
	m.agents["A2"] = g2

	m.hosted = registerOperator(t, p, scopedGRN(m.slug["A"], agent.ServiceTypeOperator, "host"))
	m.controller = registerOperator(t, p, scopedGRN(m.slug["A"], agent.ServiceTypeOperator, "ctl"))
	now := time.Now()
	terms, err := operator.ParseGrantTerms(orgGRN("A"), m.hosted, []string{m.controller.String()},
		[]string{string(operator.OperationAgentCreate), string(operator.OperationAgentConfigure)},
		now.Add(24*time.Hour), now)
	require.NoError(t, err)
	_, _, err = operatorpg.New(p).WriteGrant(ctx, operator.GrantWrite{
		Org: orgGRN("A"), Operator: m.hosted, RequestID: "mx-" + operator.RequestID(sfx),
		Terms: terms, Actor: owner, Now: now,
	}, operator.GrantID("grant-mx-"+sfx))
	require.NoError(t, err)
	m.grant, err = operatorpg.New(p).CurrentGrant(ctx, orgGRN("A"), m.hosted)
	require.NoError(t, err)
	m.subjects = append(m.subjects, subject{label: "A.operator", org: "A", grn: m.hosted, kind: "operator"})

	for _, label := range []string{"A", "B", "C"} {
		s := m.slug[label]
		m.resources = append(m.resources,
			resource{label + ".org", label, grn.Org(s), ""},
			resource{label + ".group", label, grn.New(s, org.ServiceTypeGroup, "g1"), apigen.PermissionResourceTypeGroup},
			resource{label + ".project", label, grn.New(s, org.ServiceTypeProject, "p1"), apigen.PermissionResourceTypeProject},
			resource{label + ".agent", label, m.agents[label], apigen.PermissionResourceTypeAgent},
			resource{label + ".policy", label, m.draftPolicy(t, policies, label), apigen.PermissionResourceTypePolicy})
	}
	_ = agents

	srv := api.NewServer(users, orgs, agents, nil, messagepg.New(p), nil, sessions,
		nil, nil, nil, nil, api.NewGRPCEvaluator(engine), nil, nil,
		cli.NewAuthorityService(p, engine), cli.NewStandaloneService(p, engine),
		api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv,
		apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	m.browser = browser.URL

	for _, a := range org.Actions() {
		if strings.HasPrefix(a, "iam:") {
			m.actions = append(m.actions, a)
		}
	}
	return m
}

// draftPolicy stores a policy of the organization that is never
// activated, so the platform's engine reads no statement from it while
// the resource exists in the platform.
func (m *matrix) draftPolicy(t *testing.T, srv *authz.PolicyGRPCServer, label string) grn.GRN {
	t.Helper()
	raw, err := json.Marshal(statementDoc("draft", wire.EffectAllow, org.ActionOrgRead, "grn:"+m.slug[label]+"/*"))
	require.NoError(t, err)
	created, err := srv.CreatePolicy(context.Background(), &authzv1.CreatePolicyRequest{
		OrgGrn: orgRootGRN(m.slug[label]).String(), Name: "draft", Document: string(raw),
		CreatedBy: grn.New(grn.RootOwner, user.ServiceType, "mx").String(),
	})
	require.NoError(t, err)
	g, err := grn.Parse(created.GetPolicy().GetGrn())
	require.NoError(t, err)
	return g
}

// deliverAll sends the feed until the outbox is empty, and fails if
// the service refuses any of it, since the state would not have
// arrived.
func deliverAll(t *testing.T, p *pgxpool.Pool, store *feedpg.Store, sender policyfeed.Sender) {
	t.Helper()
	for range 50 {
		result, err := store.Deliver(context.Background(), policyfeed.BatchLimit, sender)
		require.NoError(t, err)
		require.Empty(t, result.Refused, "the service refused an entity")
		require.Empty(t, result.SetAside, "the service refused a role link")
		require.Empty(t, result.ScopedSetAside, "the service refused a scoped role link")
		if len(outboxMarks(t, p)) == 0 {
			return
		}
	}
	t.Fatal("the outbox did not empty")
}

// admitted reads off the route which kinds of resource it takes each
// action on: a pair of a kind none of the action's operations address
// is a 400.
func (m *matrix) admitted(t *testing.T) map[string]map[apigen.PermissionResourceType]bool {
	t.Helper()
	owner := m.subjects[0]
	out := map[string]map[apigen.PermissionResourceType]bool{}
	for _, a := range m.actions {
		out[a] = map[apigen.PermissionResourceType]bool{}
		for _, k := range []apigen.PermissionResourceType{"", apigen.PermissionResourceTypeGroup,
			apigen.PermissionResourceTypeProject, apigen.PermissionResourceTypeAgent,
			apigen.PermissionResourceTypePolicy} {
			status, _ := m.check(t, owner.cookie, m.slug["A"], []apigen.PermissionCheck{pair(a, k, "probe")})
			require.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, status, "%s on %q", a, k)
			out[a][k] = status == http.StatusOK
		}
	}
	return out
}

func pair(action string, kind apigen.PermissionResourceType, id string) apigen.PermissionCheck {
	c := apigen.PermissionCheck{Action: action}
	if kind != "" {
		c.Resource = &apigen.PermissionResource{Type: kind, Id: id}
	}
	return c
}

func (m *matrix) check(t *testing.T, cookie *http.Cookie, orgSlug string, checks []apigen.PermissionCheck) (int, []apigen.PermissionCheckResult) {
	t.Helper()
	raw, err := json.Marshal(apigen.PermissionCheckRequest{Checks: checks})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, m.browser+"/orgs/"+orgSlug+"/permission-checks", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var body bytes.Buffer
	_, err = body.ReadFrom(resp.Body)
	require.NoError(t, err)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	var got apigen.PermissionCheckAnswer
	require.NoError(t, json.Unmarshal(body.Bytes(), &got))
	return resp.StatusCode, got.Results
}

// askPlatform builds every question and asks the platform each one by
// the path that enforces it for that kind of subject.
func (m *matrix) askPlatform(t *testing.T) []row {
	t.Helper()
	admits := m.admitted(t)
	var rows []row
	for _, s := range m.subjects {
		for _, r := range m.resources {
			var pending []int
			for _, a := range m.actions {
				if !admits[a][r.kind] {
					continue
				}
				rows = append(rows, row{
					key:      s.label + " " + a + " " + r.label,
					question: policydecision.Question{Subject: s.grn, Action: a, Resource: r.grn},
				})
				pending = append(pending, len(rows)-1)
			}
			m.decide(t, s, r, rows, pending)
		}
	}
	for _, s := range []struct{ from, to string }{
		{"A", "A2"}, {"A", "B"}, {"A", "C"}, {"B", "A"}, {"B", "C"}, {"C", "A"}, {"C", "B"},
	} {
		rows = append(rows, m.send(t, s.from, s.to))
	}
	return rows
}

// decide fills the platform's answers for the rows pending, which all
// ask subject s about resources of r's organization.
func (m *matrix) decide(t *testing.T, s subject, r resource, rows []row, pending []int) {
	t.Helper()
	switch s.kind {
	case "user":
		for chunk := range slices.Chunk(pending, pageSize) {
			checks := make([]apigen.PermissionCheck, len(chunk))
			for i, at := range chunk {
				checks[i] = pair(rows[at].question.Action, r.kind, r.grn.Identifier)
			}
			status, results := m.check(t, s.cookie, m.slug[r.org], checks)
			if s.org != r.org {
				require.Equal(t, http.StatusForbidden, status, "%s across organizations", s.label)
				for _, at := range chunk {
					rows[at].platform, rows[at].via = answerDeny, howRefused
				}
				continue
			}
			require.Equal(t, http.StatusOK, status, s.label)
			require.Len(t, results, len(chunk))
			for i, at := range chunk {
				rows[at].platform, rows[at].via = answer(results[i].Decision), howRoute
			}
		}
	case "operator":
		for _, at := range pending {
			rows[at].platform, rows[at].via = m.operatorAnswer(rows[at].question.Action, s.org == r.org)
		}
	default:
		for _, at := range pending {
			rows[at].platform, rows[at].via = answerDeny, howNoRoute
		}
	}
}

// operatorAnswer is the grant's answer to an operation of the
// operator's organization, and a deny to anything it does not name.
func (m *matrix) operatorAnswer(action string, sameOrg bool) (answer, how) {
	op, err := operator.ParseOperation(action)
	if err != nil {
		return answerDeny, howNoRoute
	}
	if sameOrg && m.grant.Covers(op, m.controller, time.Now()) {
		return answerAllow, howGrant
	}
	return answerDeny, howGrant
}

// send asks the machine listener to carry one message from the agent
// of one organization to another and reads the answer off its status.
func (m *matrix) send(t *testing.T, from, to string) row {
	t.Helper()
	recipient := m.agents[to]
	status, body := machineSend(t, m.senders[from], http.MethodPost, m.machine+"/messages",
		submissionTo(recipient, "mx-"+from+"-"+to))
	r := row{
		key:      from + ".agent iam:message.send " + to + ".agent",
		question: policydecision.Question{Subject: m.agents[from], Action: "iam:message.send", Resource: recipient},
		via:      howMachine,
	}
	switch status {
	case http.StatusCreated:
		// A send across a boundary no consent covers is answered 201
		// with garam's refusal, stored as a message from the recipient
		// to the sender (ADR-0013), so the sender named is the answer.
		var stored machinegen.Message
		require.NoError(t, json.Unmarshal(body, &stored))
		r.platform = answerDeny
		if stored.Sender == m.agents[from].String() {
			r.platform = answerAllow
		}
	case http.StatusForbidden:
		r.platform = answerDeny
	default:
		t.Fatalf("send %s to %s answered %d: %s", from, to, status, body)
	}
	return r
}

// askService asks the policy service every question in batches.
func (m *matrix) askService(t *testing.T, service policydecision.Service, rows []row) {
	t.Helper()
	for start := 0; start < len(rows); start += matrixBatch {
		end := min(start+matrixBatch, len(rows))
		qs := make([]policydecision.Question, 0, end-start)
		for _, r := range rows[start:end] {
			qs = append(qs, r.question)
		}
		decisions, err := service.Evaluations(context.Background(), qs)
		require.NoError(t, err)
		for i, d := range decisions {
			rows[start+i].service = d
		}
	}
}

// probeAgrees asks a sample of the questions again, one at a time and
// as a probe, and holds the service to the answer it gave in a batch.
//
// A question the service refuses is a denial with one reason that
// names no policy inside a batch, and an invalid argument when asked
// alone.
func probeAgrees(t *testing.T, ctx context.Context, service policydecision.Service, rows []row) {
	t.Helper()
	sampled, refused := 0, 0
	for i, r := range rows {
		if i%max(1, len(rows)/60) != 0 && r.agrees() && !r.refused() {
			continue
		}
		sampled++
		for _, probe := range []bool{false, true} {
			q := r.question
			q.Probe = probe
			d, err := service.Evaluate(ctx, q)
			if r.refused() {
				refused++
				require.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%s: %v", r.key, err)
				assert.False(t, d.Allowed, r.key)
				continue
			}
			require.NoError(t, err, r.key)
			assert.Equal(t, r.service.Allowed, d.Allowed, "%s probe=%t", r.key, probe)
			assert.Equal(t, r.service.Reasons, d.Reasons, "%s probe=%t", r.key, probe)
		}
	}
	t.Logf("asked %d questions again one at a time, %d refused", sampled, refused/2)
}

// assertNoNonAgentSender holds what the matrix reports about the
// machine listener's send: an operator's token carries no serial, and
// only an agent's does, so the operator is refused before a policy is
// asked while an agent beside it is carried.
func (m *matrix) assertNoNonAgentSender(t *testing.T) {
	t.Helper()
	status, body := machineSend(t, m.operators["A"], http.MethodPost, m.machine+"/messages",
		submissionTo(m.agents["A2"], "mx-operator"))
	assert.Equal(t, http.StatusUnauthorized, status, string(body))
	status, body = machineSend(t, m.senders["A"], http.MethodPost, m.machine+"/messages",
		submissionTo(m.agents["A2"], "mx-agent-control"))
	assert.Equal(t, http.StatusCreated, status, string(body))
}
