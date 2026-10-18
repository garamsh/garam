package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// signedClaims is what a verifier reads out of a message signature,
// declared here rather than taken from garam: the verifier below holds
// no garam code, which is what ADR-0101 promises a verifier needs.
type signedClaims struct {
	Iss           string `json:"iss"`
	Sub           string `json:"sub"`
	Aud           string `json:"aud"`
	Org           string `json:"org"`
	MessageID     string `json:"message_id"`
	ContentSHA256 string `json:"content_sha256"`
	Iat           int64  `json:"iat"`
	Exp           int64  `json:"exp"`
}

// verify is a verifier made of a stock JOSE library and the key set
// alone, performing ADR-0101's checks against the identity it is
// configured to be: the issuer it trusts, the audience it answers to,
// and the identifier a message arrived under. It answers the claims, or
// why it refused.
func verify(set jose.JSONWebKeySet, issuer, audience, arrivedAs, content, signature string) (signedClaims, error) {
	if err := headerHoldsOnly(signature, "alg", "kid", "typ"); err != nil {
		return signedClaims{}, err
	}
	signed, err := jose.ParseSigned(signature, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return signedClaims{}, err
	}
	if len(signed.Signatures) != 1 {
		return signedClaims{}, errors.New("not one signature")
	}
	header := signed.Signatures[0].Protected
	if typ, ok := header.ExtraHeaders[jose.HeaderType].(string); !ok || typ != "agent-message+jwt" {
		return signedClaims{}, errors.New("typ is not agent-message+jwt")
	}
	keys := set.Key(header.KeyID)
	if len(keys) != 1 {
		return signedClaims{}, errors.New("kid names no published key")
	}
	payload, err := signed.Verify(keys[0])
	if err != nil {
		return signedClaims{}, err
	}
	var claims signedClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return signedClaims{}, err
	}
	now := time.Now()
	digest := sha256.Sum256([]byte(content))
	switch {
	case claims.Iss != issuer:
		return claims, errors.New("issuer not trusted")
	case claims.Aud != audience:
		return claims, errors.New("addressed elsewhere")
	case !time.Unix(claims.Exp, 0).After(now.Add(-time.Minute)):
		return claims, errors.New("expired")
	case !time.Unix(claims.Iat, 0).Before(now.Add(time.Minute)):
		return claims, errors.New("signed in the future")
	case claims.ContentSHA256 != base64.RawURLEncoding.EncodeToString(digest[:]):
		return claims, errors.New("content does not match its digest")
	case claims.MessageID != arrivedAs:
		return claims, errors.New("signed for another identifier")
	}
	return claims, nil
}

// headerHoldsOnly refuses a protected header holding any member but
// these: a verifier takes no key, and no place to fetch one, from the
// message it is judging (ADR-0101), and go-jose reads `jku` and `jwk`
// without being asked whether they may be there.
func headerHoldsOnly(signature string, members ...string) error {
	encoded, _, ok := strings.Cut(signature, ".")
	if !ok {
		return errors.New("not a compact JWS")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if len(header) != len(members) {
		return errors.New("the header holds a member beside alg, kid and typ")
	}
	for _, m := range members {
		if _, ok := header[m]; !ok {
			return errors.New("the header lacks " + m)
		}
	}
	return nil
}

// keySet reads the published set the way a verifier anywhere does: with
// no certificate, from the issuer's path.
func keySet(t *testing.T, url string) jose.JSONWebKeySet {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Get(url + "/message-signing-keys")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Equal(t, "application/jwk-set+json", resp.Header.Get("Content-Type"))
	assert.Equal(t, "max-age=3600", resp.Header.Get("Cache-Control"))
	var set jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(body, &set))
	return set
}

// signingStack is one organization's listener with two agents of it,
// one to send and one to receive.
type signingStack struct {
	pool         *pgxpool.Pool
	sealer       ca.Sealer
	engine       grpc.ClientConnInterface
	url          string
	sender       *http.Client
	recipient    *http.Client
	senderGRN    grn.GRN
	recipientGRN grn.GRN
}

func newSigningStack(t *testing.T) signingStack {
	t.Helper()
	return newSigningStackHolding(t, nil)
}

// newSigningStackHolding is newSigningStack with its signing keys
// created in signing, or sealed into their rows where signing is nil
// (ADR-0104).
func newSigningStackHolding(t *testing.T, signing message.Custodian) signingStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	_, engine := policyEngine(t, p)
	url := machineListenerHolding(t, p, sealer, engine, signing)
	operator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	return signingStack{pool: p, sealer: sealer, engine: engine, url: url, sender: sender, recipient: recipient, senderGRN: senderGRN, recipientGRN: recipientGRN}
}

// post stores one message from the sender to the recipient.
func (s signingStack) post(t *testing.T, body string) machinegen.Message {
	t.Helper()
	status, answer := machineSend(t, s.sender, http.MethodPost, s.url+"/messages",
		`{"recipient":"`+s.recipientGRN.String()+`","idempotencyKey":"key-1","body":"`+body+`"}`)
	require.Equal(t, http.StatusCreated, status, string(answer))
	var posted machinegen.Message
	require.NoError(t, json.Unmarshal(answer, &posted))
	return posted
}

// claim hands the recipient its next message.
func (s signingStack) claim(t *testing.T) machinegen.Claim {
	t.Helper()
	status, answer := machineSend(t, s.recipient, http.MethodPost, s.url+"/agents/"+s.recipientGRN.String()+"/claims", "")
	require.Equal(t, http.StatusOK, status, string(answer))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(answer, &claimed))
	require.NotNil(t, claimed.Content, "the claim carried no content")
	require.NotNil(t, claimed.Signature, "the claim carried no signature")
	return claimed
}

// TestMessageSignature_AStockJOSELibraryVerifiesAHandOutAgainstThePublishedSetAlone
// holds ADR-0101's format end to end: a claim carries the content and
// garam's signature, and a verifier holding nothing but go-jose and the
// set garam publishes accepts it, reading the claims ADR-0101 lists.
// The content is also the adapter's own encoding of the message, which
// is what lets the adapter forward it unchanged (ADR-0101 step 2).
func TestMessageSignature_AStockJOSELibraryVerifiesAHandOutAgainstThePublishedSetAlone(t *testing.T) {
	s := newSigningStack(t)
	posted := s.post(t, "hello")
	claimed := s.claim(t)
	set := keySet(t, s.url)
	require.Len(t, set.Keys, 1, "the deployment's first key is the one published")
	assert.Equal(t, "ES256", set.Keys[0].Algorithm)
	assert.Equal(t, "sig", set.Keys[0].Use)

	claims, err := verify(set, string(testIssuer), s.recipientGRN.String(), claimed.Message.Id, *claimed.Content, *claimed.Signature)
	require.NoError(t, err)

	assert.Equal(t, posted.Id, claims.MessageID)
	assert.Equal(t, s.senderGRN.String(), claims.Sub, "the sender garam vouches for is the one its listener authenticated")
	assert.Equal(t, "acme", claims.Org)
	assert.Equal(t, briefDelivery().LeaseCeiling, time.Duration(claims.Exp-claims.Iat)*time.Second,
		"a signature is valid for the lease ceiling and no longer")
	assert.Equal(t, `{"contract":"garam-message.v1","sender":"`+s.senderGRN.String()+`","body":"hello"}`, *claimed.Content)
	adapters, err := (&delivery.Claim{Sender: s.senderGRN, Body: "hello"}).Content()
	require.NoError(t, err)
	assert.Equal(t, adapters, *claimed.Content, "garam's content is the adapter's own encoding")
}

// TestMessageSignature_EveryAttemptCarriesTheSameContentAndIdentifier
// holds what lets a runtime's replay check survive signing: the lease
// on the first claim lapses unreported, the message is handed out
// again, and the second attempt carries a signature of its own over the
// same content, identifier, sender, audience and organization.
func TestMessageSignature_EveryAttemptCarriesTheSameContentAndIdentifier(t *testing.T) {
	s := newSigningStack(t)
	s.post(t, "hello")
	first := s.claim(t)
	// The next claim waits for the lease to lapse, which is the
	// redelivery ADR-0018 makes of an attempt nobody reported.
	var second machinegen.Claim
	require.Eventually(t, func() bool {
		status, answer := machineSend(t, s.recipient, http.MethodPost, s.url+"/agents/"+s.recipientGRN.String()+"/claims", "")
		if status != http.StatusOK {
			return false
		}
		return json.Unmarshal(answer, &second) == nil
	}, 3*briefDelivery().LeaseCeiling, 100*time.Millisecond)
	require.Equal(t, 1, first.Attempt)
	require.Equal(t, 2, second.Attempt, "the setup handed the message out a second time")
	require.NotNil(t, second.Content)
	require.NotNil(t, second.Signature)

	set := keySet(t, s.url)
	one, err := verify(set, string(testIssuer), s.recipientGRN.String(), first.Message.Id, *first.Content, *first.Signature)
	require.NoError(t, err)
	two, err := verify(set, string(testIssuer), s.recipientGRN.String(), second.Message.Id, *second.Content, *second.Signature)
	require.NoError(t, err)

	assert.Equal(t, *first.Content, *second.Content)
	assert.NotEqual(t, *first.Signature, *second.Signature, "each attempt is signed afresh")
	one.Iat, one.Exp, two.Iat, two.Exp = 0, 0, 0, 0
	assert.Equal(t, one, two, "everything but the times is the same on both attempts")
}

// TestMessageSignature_AVerifierRefusesATamperedContentByte holds the
// digest's binding: one byte changed in the content is refused, while
// the same content unchanged under the same signature is accepted, so
// what refused is the digest and not a signature that never verified.
func TestMessageSignature_AVerifierRefusesATamperedContentByte(t *testing.T) {
	s := newSigningStack(t)
	s.post(t, "hello")
	claimed := s.claim(t)
	set := keySet(t, s.url)
	verifyContent := func(content string) error {
		_, err := verify(set, string(testIssuer), s.recipientGRN.String(), claimed.Message.Id, content, *claimed.Signature)
		return err
	}

	require.NoError(t, verifyContent(*claimed.Content), "the untampered content is accepted")

	tampered := []byte(*claimed.Content)
	tampered[len(tampered)-3] ^= 0x01
	assert.EqualError(t, verifyContent(string(tampered)), "content does not match its digest")
}

// TestMessageSignature_GaramSignsTheRefusalItWroteAsItsOwn holds the
// one message no caller posted: the cross-organization refusal keeps the
// intended recipient as the content's sender (ADR-0035), and its
// signature names garam's issuer as the sender garam vouches for. A
// message the same sender posted inside its organization is the
// control, signed under the caller its listener authenticated.
func TestMessageSignature_GaramSignsTheRefusalItWroteAsItsOwn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	for _, name := range []string{"acme", "globex"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	acmeOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	globexOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("globex", agent.ServiceTypeOperator, "one")))
	recipientGRN, _ := provisioned(t, url, acmeOperator)
	strangerGRN, stranger := provisioned(t, url, globexOperator)
	siblingGRN, sibling := provisioned(t, url, globexOperator)

	status, body := machineSend(t, stranger, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))
	var refusal machinegen.Message
	require.NoError(t, json.Unmarshal(body, &refusal))
	require.Equal(t, strangerGRN.String(), refusal.Recipient, "the setup stored a refusal back to the sender")

	status, body = machineSend(t, stranger, http.MethodPost, url+"/agents/"+strangerGRN.String()+"/claims", "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	require.Equal(t, refusal.Id, claimed.Message.Id)
	set := keySet(t, url)
	claims, err := verify(set, string(testIssuer), strangerGRN.String(), claimed.Message.Id, *claimed.Content, *claimed.Signature)
	require.NoError(t, err)
	assert.Equal(t, string(testIssuer), claims.Sub, "garam vouches for itself as the refusal's sender")
	assert.Contains(t, *claimed.Content, `"sender":"`+recipientGRN.String()+`"`, "the content keeps the stored sender")

	// The control: a message inside globex is signed under its poster.
	status, body = machineSend(t, sibling, http.MethodPost, url+"/messages", submissionTo(strangerGRN, "key-2"))
	require.Equal(t, http.StatusCreated, status, string(body))
	status, body = machineSend(t, stranger, http.MethodPost, url+"/agents/"+strangerGRN.String()+"/claims/"+claimed.Id+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))
	status, body = machineSend(t, stranger, http.MethodPost, url+"/agents/"+strangerGRN.String()+"/claims", "")
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &claimed))
	claims, err = verify(set, string(testIssuer), strangerGRN.String(), claimed.Message.Id, *claimed.Content, *claimed.Signature)
	require.NoError(t, err)
	assert.Equal(t, siblingGRN.String(), claims.Sub)
}

// TestMessageSigningKeys_ConcurrentRotationsStoreOneKey holds the
// rotation lock against Postgres: every replica that finds no key
// signing decides again once it holds the lock, on the clock as it then
// stands, so however many rotate at once one key is stored, and that
// key begins no earlier than the lock was released. The test holds the
// lock itself until every rotation is waiting on it. A decision instant
// fixed when each transaction began would precede the release for every
// one of them, so the key's start is what fails deterministically; the
// count fails only when the waiters reach the lock out of the order
// they began in, which the scheduler decides.
func TestMessageSigningKeys_ConcurrentRotationsStoreOneKey(t *testing.T) {
	ctx := context.Background()
	const replicas = 8
	// The lock's holder, every waiting rotation and the poll counting
	// them hold a connection at once. pgxpool's default MaxConns is
	// max(4, NumCPU), which on a runner with few cores fits fewer.
	cfg := pool(t).Config()
	cfg.MaxConns = replicas + 2
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	key := make([]byte, ca.KeySize)
	_, err = rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	require.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM message_signing_keys`), "the setup started with no key")

	holder, err := p.Acquire(ctx)
	require.NoError(t, err)
	defer holder.Release()
	_, err = holder.Exec(ctx, `SELECT pg_advisory_lock(hashtext('message_signing_keys'))`)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, replicas)
	for i := range replicas {
		ring, err := message.NewKeyring(message.NewPostgresStore(p), testIssuer, message.Config{}, message.NewFileCustodian(sealer))
		require.NoError(t, err)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = ring.PublishedKeys(ctx)
		}()
	}
	require.Eventually(t, func() bool {
		return countRows(t, p, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
			AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`) == replicas
	}, 10*time.Second, 20*time.Millisecond, "every rotation was waiting on the lock")

	var released time.Time
	require.NoError(t, holder.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&released))
	_, err = holder.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('message_signing_keys'))`)
	require.NoError(t, err)
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM message_signing_keys`))
	var signsFrom time.Time
	require.NoError(t, p.QueryRow(ctx, `SELECT signs_from FROM message_signing_keys`).Scan(&signsFrom))
	assert.False(t, signsFrom.Before(released),
		"the key begins at %s, before the lock was released at %s: the rotation decided on a clock read before it held the lock", signsFrom, released)
}

// unsigned signs nothing, for a test of the queue whose subject is not
// the signature.
type unsigned struct{}

func (unsigned) Sign(context.Context, *message.Claim) (message.Signed, error) {
	return message.Signed{}, nil
}
