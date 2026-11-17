//go:build policyservice

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	relaygrpc "github.com/garamsh/garam/platform/internal/policyrelay/grpc"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// The case here holds a send between two organizations against the
// stack's real policy service (ADR-0168): the machine listener asks it
// whether the sender may send, and it decides from the consents the
// platform delivered after each organization granted or withdrew its
// half through the consent API (ADR-0159).

// consentedStack is the machine listener and the browser listener over
// the real policy service, with an agent of acme that sends and three
// agents of globex that are sent to.
type consentedStack struct {
	p          *pgxpool.Pool
	machine    string
	browser    string
	acmeOwner  *http.Cookie
	globexOwn  *http.Cookie
	sender     *http.Client
	senderGRN  grn.GRN
	recipients []recipientOf
}

// recipientOf is an agent of globex and the client it reads its queue
// as.
type recipientOf struct {
	grn    grn.GRN
	client *http.Client
}

func newConsentedStack(t *testing.T) *consentedStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	rp := startRealPolicy(t, p)
	orgs, users := orgpg.New(p), userpg.New(p)
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(globex, "globex")))
	owner := func(id string, home grn.GRN) *http.Cookie {
		g := grn.New(grn.RootOwner, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(home, g, org.RoleOwner)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}
	}
	s := &consentedStack{p: p, acmeOwner: owner("ada", acme), globexOwn: owner("cy", globex)}

	_, sealer := keyEncryptionKey(t)
	s.machine = machineListenerDeciding(t, p, sealer, nil, rp.asked)
	acmeOperator := operatorClient(t, p, s.machine, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	globexOperator := operatorClient(t, p, s.machine, scopedGRN("globex", agent.ServiceTypeOperator, "one"))
	s.senderGRN, s.sender = provisioned(t, s.machine, acmeOperator)
	for range 3 {
		g, client := provisioned(t, s.machine, globexOperator)
		s.recipients = append(s.recipients, recipientOf{grn: g, client: client})
	}
	rp.deliver(t)

	// The delivery loop `serve api` runs, whose gate holds a consent
	// route's answer until the service holds what it changed.
	svc := policyfeed.NewService(rp.store, rp.sender, slog.New(slog.DiscardHandler),
		policyfeed.Config{Poll: time.Hour, MaxBackoff: 100 * time.Millisecond})
	loop, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = svc.Run(loop) }()
	t.Cleanup(func() { stop(); <-done })
	gate := policyfeed.NewGate(rp.store, svc, 10*time.Second)

	srv := api.NewServer(users, orgs, nil, nil, messagepg.New(p), nil, authnpg.NewSessionStore(p),
		nil, nil, nil, nil, rp.asked, nil, nil, nil, api.Config{SessionTTL: time.Hour}).
		WithPolicyRelay(relaygrpc.New(rp.client)).WithFeed(gate)
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	s.browser = browser.URL
	return s
}

// browse makes one request of the browser listener as cookie.
func (s *consentedStack) browse(t *testing.T, method, path string, cookie *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.browser+path, nil)
	require.NoError(t, err)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// halves reads the consent between acme and globex back through the
// API, as acme sees it: whether acme and globex have each granted.
func (s *consentedStack) halves(t *testing.T) (acme, globex bool) {
	t.Helper()
	status, got := s.browse(t, http.MethodGet, "/orgs/acme/consents", s.acmeOwner)
	require.Equal(t, http.StatusOK, status, got)
	var consents []apigen.OrgConsent
	require.NoError(t, json.Unmarshal([]byte(got), &consents))
	for _, c := range consents {
		if c.OrgGrn == orgRootGRN("globex").String() {
			return c.GrantedHere, c.GrantedThere
		}
	}
	return false, false
}

// send posts one message from the sender to recipient and answers the
// message the listener answered with.
func (s *consentedStack) send(t *testing.T, recipient grn.GRN, key string) machinegen.Message {
	t.Helper()
	status, body := machineSend(t, s.sender, http.MethodPost, s.machine+"/messages", submissionTo(recipient, key))
	require.Equal(t, http.StatusCreated, status, string(body))
	var answered machinegen.Message
	require.NoError(t, json.Unmarshal(body, &answered))
	return answered
}

// storedFor counts the messages the sender stored for recipient.
func (s *consentedStack) storedFor(t *testing.T, recipient grn.GRN) int {
	t.Helper()
	return countRows(t, s.p, "SELECT count(*) FROM messages WHERE sender_grn = $1 AND recipient_grn = $2",
		s.senderGRN.String(), recipient.String())
}

// denials reads globex's audit records as its owner, which hold the
// denials of a send to one of its agents, and answers the pairs of
// sender and recipient the service denied. A denial is recorded after
// it is answered, so the read waits until the denial of every one of
// recipients shows or the bound passes, and one that never shows is
// answered as it stands.
func (s *consentedStack) denials(t *testing.T, recipients ...grn.GRN) map[[2]string]apigen.AuditRecord {
	t.Helper()
	read := func() map[[2]string]apigen.AuditRecord {
		out := map[[2]string]apigen.AuditRecord{}
		query := "?pageSize=100"
		for {
			status, got := s.browse(t, http.MethodGet, "/orgs/globex/audit-records"+query, s.globexOwn)
			require.Equal(t, http.StatusOK, status, got)
			var page apigen.AuditRecordPage
			require.NoError(t, json.Unmarshal([]byte(got), &page))
			for _, r := range page.Records {
				if r.Kind == apigen.Denial {
					out[[2]string{r.Subject, r.Resource}] = r
				}
			}
			if page.NextPageToken == nil {
				return out
			}
			query = "?pageSize=100&pageToken=" + *page.NextPageToken
		}
	}
	var got map[[2]string]apigen.AuditRecord
	assert.Eventually(t, func() bool {
		got = read()
		for _, r := range recipients {
			if _, shown := got[[2]string{s.senderGRN.String(), r.String()}]; !shown {
				return false
			}
		}
		return true
	}, 10*time.Second, 100*time.Millisecond, "the service recorded its denial of the sends to %v", recipients)
	return got
}

// TestConsentedSend_TheServiceDecidesAcrossTheBoundaryFromTheConsentsDelivered
// sends from an agent of acme to agents of globex through the machine
// listener while the consent between the two organizations changes
// through the API: with only acme's half granted the send is refused,
// with both halves it is accepted and stored for its recipient, and
// once globex withdraws its half it is refused again. Each refusal
// stores nothing for its recipient and is the service's own, shown by
// its audit record of the denial; the accepted send is the control, for
// it leaves no denial and shows the refusals are the missing half and
// not a listener that refuses every crossing.
func TestConsentedSend_TheServiceDecidesAcrossTheBoundaryFromTheConsentsDelivered(t *testing.T) {
	s := newConsentedStack(t)
	missing, granted, withdrawn := s.recipients[0], s.recipients[1], s.recipients[2]

	status, got := s.browse(t, http.MethodPut, "/orgs/acme/consents/globex", s.acmeOwner)
	require.Equal(t, http.StatusOK, status, got)
	acmeHalf, globexHalf := s.halves(t)
	require.True(t, acmeHalf, "setup: acme granted its half")
	require.False(t, globexHalf, "setup: globex has not granted its")

	refusal := s.send(t, missing.grn, "missing")
	assert.Equal(t, s.senderGRN.String(), refusal.Recipient, "with one half missing the refusal came back to the sender")
	assert.Equal(t, 0, s.storedFor(t, missing.grn), "with one half missing nothing was stored for the recipient")

	status, got = s.browse(t, http.MethodPut, "/orgs/globex/consents/acme", s.globexOwn)
	require.Equal(t, http.StatusOK, status, got)
	acmeHalf, globexHalf = s.halves(t)
	require.True(t, acmeHalf && globexHalf, "setup: both halves stand")

	accepted := s.send(t, granted.grn, "granted")
	assert.Equal(t, granted.grn.String(), accepted.Recipient, "with both halves the send was accepted")
	assert.Equal(t, 1, s.storedFor(t, granted.grn), "with both halves the message was stored for the recipient")
	status, body := machineSend(t, granted.client, http.MethodPost,
		s.machine+"/agents/"+pathEscape(granted.grn.String())+"/claims", "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, accepted.Id, claimed.Message.Id, "the recipient holds the message")

	status, got = s.browse(t, http.MethodDelete, "/orgs/globex/consents/acme", s.globexOwn)
	require.Equal(t, http.StatusNoContent, status, got)
	acmeHalf, globexHalf = s.halves(t)
	require.True(t, acmeHalf, "setup: acme's half stands")
	require.False(t, globexHalf, "setup: globex withdrew its half")

	refusal = s.send(t, withdrawn.grn, "withdrawn")
	assert.Equal(t, s.senderGRN.String(), refusal.Recipient, "after the withdrawal the refusal came back to the sender")
	assert.Equal(t, 0, s.storedFor(t, withdrawn.grn), "after the withdrawal nothing was stored for the recipient")

	denied := s.denials(t, missing.grn, withdrawn.grn)
	for name, r := range map[string]recipientOf{"missing half": missing, "withdrawn half": withdrawn} {
		record, ok := denied[[2]string{s.senderGRN.String(), r.grn.String()}]
		require.True(t, ok, "the service's audit holds its denial of the send with a %s", name)
		assert.Equal(t, "deny", record.Outcome, name)
		assert.Equal(t, "iam:message.send", record.Action, name)
	}
	assert.NotContains(t, denied, [2]string{s.senderGRN.String(), granted.grn.String()},
		"control: the accepted send is no denial of the service's")
}
