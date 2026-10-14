package operator_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// exchanges is the TokenStore in memory: credentials by hash, the
// tokens recorded under them, and the live tokens by hash with how
// many times one was read.
type exchanges struct {
	credentials map[string]operator.CredentialHolder
	recorded    []*operator.Token
	live        map[string]operator.TokenHolder
	resolved    int
}

func newExchanges() *exchanges {
	return &exchanges{credentials: map[string]operator.CredentialHolder{}, live: map[string]operator.TokenHolder{}}
}

func (e *exchanges) CredentialHolder(_ context.Context, credentialHash string) (operator.CredentialHolder, error) {
	holder, ok := e.credentials[credentialHash]
	if !ok {
		return operator.CredentialHolder{}, operator.ErrCredentialInvalid
	}
	return holder, nil
}

func (e *exchanges) RecordToken(_ context.Context, _ operator.CredentialHolder, t *operator.Token) error {
	e.recorded = append(e.recorded, t)
	return nil
}

// VerificationKey and RecordAssertionToken are the key half, which
// key_test.go exercises over its own store; this one holds no key.
func (e *exchanges) VerificationKey(context.Context, grn.GRN, string) (operator.Key, error) {
	return operator.Key{}, operator.ErrClientInvalid
}

func (e *exchanges) RecordAssertionToken(context.Context, operator.VerifiedAssertion, *operator.Token) error {
	return operator.ErrClientInvalid
}

func (e *exchanges) ResolveToken(_ context.Context, tokenHash string) (operator.TokenHolder, error) {
	e.resolved++
	holder, ok := e.live[tokenHash]
	if !ok {
		return operator.TokenHolder{}, operator.ErrTokenInvalid
	}
	return holder, nil
}

// issued registers a credential for subject in store and answers the
// value an operator would present.
func issued(t *testing.T, store *exchanges, subject grn.GRN) string {
	t.Helper()
	credential, err := operator.NewIssuedCredential(subject, grn.New("acme", grn.DefaultProject, "user", "u1"))
	require.NoError(t, err)
	store.credentials[credential.Hash()] = operator.CredentialHolder{ID: 1, Operator: subject}
	return credential.Value()
}

func exchanging(t *testing.T, store *exchanges) (operator.Service, time.Time) {
	t.Helper()
	at := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	svc := operator.NewService(newRecords(), newTokens(), store, &signer{},
		operator.Config{Now: func() time.Time { return at }})
	return svc, at
}

func TestNewIssuedCredential_IsPrefixed256BitsAndStoredAsItsHash(t *testing.T) {
	credential, err := operator.NewIssuedCredential(operatorGRN("acme", "one"), grn.New("acme", grn.DefaultProject, "user", "u1"))
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(credential.Value(), operator.CredentialPrefix))
	assert.Len(t, strings.TrimPrefix(credential.Value(), operator.CredentialPrefix), 64, "256 bits in hex")
	assert.Equal(t, operator.HashToken(credential.Value()), credential.Hash())
	assert.NotContains(t, credential.Hash(), credential.Value())
}

func TestNewIssuedCredential_RefusesASubjectThatIsNotAnOperator(t *testing.T) {
	_, err := operator.NewIssuedCredential(agentGRN("acme", "a1"), grn.New("acme", grn.DefaultProject, "user", "u1"))
	require.ErrorIs(t, err, operator.ErrNotAnOperator)
}

func TestService_ExchangeAnswersAnHourLongTokenForTheCredentialsOperator(t *testing.T) {
	store := newExchanges()
	subject := operatorGRN("acme", "one")
	credential := issued(t, store, subject)
	svc, at := exchanging(t, store)

	token, err := svc.Exchange(context.Background(), credential, nil)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(token.Value, operator.TokenPrefix))
	assert.Equal(t, subject, token.Operator)
	assert.Nil(t, token.Audience)
	assert.Equal(t, at.Add(time.Hour), token.ExpiresAt)
	require.Len(t, store.recorded, 1)
	assert.Equal(t, token.Hash(), store.recorded[0].Hash())
}

func TestService_ExchangeRefusesACredentialNoOperatorHolds(t *testing.T) {
	store := newExchanges()
	// The control: the same store answers a credential it holds.
	held := issued(t, store, operatorGRN("acme", "one"))
	svc, _ := exchanging(t, store)

	_, err := svc.Exchange(context.Background(), held, nil)
	require.NoError(t, err)
	_, err = svc.Exchange(context.Background(), operator.CredentialPrefix+strings.Repeat("0", 64), nil)
	require.ErrorIs(t, err, operator.ErrCredentialInvalid)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
}

func TestService_ExchangeMintsForAnotherOperatorOfTheSameOrganizationAlone(t *testing.T) {
	store := newExchanges()
	subject := operatorGRN("acme", "controller")
	credential := issued(t, store, subject)
	svc, _ := exchanging(t, store)

	hosted := operatorGRN("acme", "control")
	token, err := svc.Exchange(context.Background(), credential, &hosted)
	require.NoError(t, err)
	assert.Equal(t, &hosted, token.Audience)

	for name, audience := range map[string]grn.GRN{
		"itself":          subject,
		"another org":     operatorGRN("other", "control"),
		"not an operator": agentGRN("acme", "a1"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Exchange(context.Background(), credential, &audience)
			require.ErrorIs(t, err, operator.ErrAudienceInvalid)
		})
	}
	assert.Len(t, store.recorded, 1, "a refused audience records no token")
}

// TestService_RenewRefusesACallerThatPresentedNoCertificate holds the
// renewal's own refusal of a token's holder (ADR-0100): a credential
// with no serial and no start is refused before anything is signed.
// The certificate inside its window is the control: the same service
// renews it, so what refuses the first is the missing certificate and
// not a window everything fails.
func TestService_RenewRefusesACallerThatPresentedNoCertificate(t *testing.T) {
	issuer := &signer{}
	svc, c := service(t, newRecords(), issuer)
	subject := operatorGRN("acme", "one")
	now := c.now()

	_, err := svc.Renew(context.Background(), operator.Credential{Subject: subject, NotAfter: now.Add(time.Hour)})
	require.ErrorIs(t, err, operator.ErrNoCertificate)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))

	_, err = svc.Renew(context.Background(), operator.Credential{
		Subject: subject, Serial: "1", NotBefore: now.Add(-lifetime + time.Hour), NotAfter: now.Add(time.Hour),
	})
	assert.NoError(t, err, "a certificate inside its window renews")
}

// TestService_ExchangeRefusesAMalformedCredentialWithoutAReadForIt
// holds that a value no issuance could have produced is refused before
// the store is asked. The well-formed unknown credential beside it is
// asked, so what spared the store is the format.
func TestService_ExchangeRefusesAMalformedCredentialWithoutAReadForIt(t *testing.T) {
	store := &countingExchanges{exchanges: newExchanges()}
	svc := operator.NewService(newRecords(), newTokens(), store, &signer{}, operator.Config{})

	for _, credential := range []string{
		"garam_ot_" + strings.Repeat("0", 64),
		operator.CredentialPrefix + strings.Repeat("0", 63),
		operator.CredentialPrefix + strings.Repeat("Z", 64),
	} {
		_, err := svc.Exchange(context.Background(), credential, nil)
		require.ErrorIs(t, err, operator.ErrCredentialInvalid)
	}
	assert.Equal(t, 0, store.asked)

	_, err := svc.Exchange(context.Background(), operator.CredentialPrefix+strings.Repeat("0", 64), nil)
	require.ErrorIs(t, err, operator.ErrCredentialInvalid)
	assert.Equal(t, 1, store.asked)
}

// countingExchanges counts the credentials the store was asked about.
type countingExchanges struct {
	*exchanges
	asked int
}

func (c *countingExchanges) CredentialHolder(ctx context.Context, hash string) (operator.CredentialHolder, error) {
	c.asked++
	return c.exchanges.CredentialHolder(ctx, hash)
}

// holds the controller proof's token half (ADR-0100): a token proves
// the operator it names only to the audience it was minted to be shown
// to. The token shown to the caller is the control, proven, so each
// refusal beside it is the one property its case changes. A value no
// exchange could have produced is refused before any read.
func TestService_VerifyShownProvesAnOperatorOnlyToTheAudienceItsTokenNames(t *testing.T) {
	controller, hosted := operatorGRN("acme", "controller"), operatorGRN("acme", "hosted")
	other := operatorGRN("acme", "other")
	store := newExchanges()
	live := func(holder operator.TokenHolder) string {
		value := operator.TokenPrefix + strings.Repeat(string(rune('a'+len(store.live))), 64)
		store.live[operator.HashToken(value)] = holder
		return value
	}
	shown := live(operator.TokenHolder{Operator: controller, Audience: &hosted})
	elsewhere := live(operator.TokenHolder{Operator: controller, Audience: &other})
	garams := live(operator.TokenHolder{Operator: controller})
	svc, _ := exchanging(t, store)
	ctx := context.Background()

	require.NoError(t, svc.VerifyShown(ctx, controller, hosted, shown), "control: the token shown to the caller proves it")
	for name, tc := range map[string]struct {
		subject, audience grn.GRN
		token             string
	}{
		"shown to another audience": {controller, hosted, elsewhere},
		"garam's own token":         {controller, hosted, garams},
		"naming another operator":   {other, hosted, shown},
		"presented by another":      {controller, other, shown},
		"not live":                  {controller, hosted, operator.TokenPrefix + strings.Repeat("f", 64)},
	} {
		assert.ErrorIs(t, svc.VerifyShown(ctx, tc.subject, tc.audience, tc.token), operator.ErrTokenNotProven, name)
	}

	read := store.resolved
	for _, malformed := range []string{shown[:len(shown)-1], operator.CredentialPrefix + strings.Repeat("0", 64), ""} {
		assert.ErrorIs(t, svc.VerifyShown(ctx, controller, hosted, malformed), operator.ErrTokenNotProven)
	}
	assert.Equal(t, read, store.resolved, "a malformed token reads nothing")
}

// testSource is the client address every exchange below arrives from,
// unless a test names another.
const testSource = "192.0.2.10"

// keyExchanges is the TokenStore's key half in memory: one live key,
// and the assertions admitted under it with the tokens they recorded.
type keyExchanges struct {
	*exchanges
	key      operator.Key
	admitted []operator.VerifiedAssertion
}

func (k *keyExchanges) VerificationKey(_ context.Context, owner grn.GRN, kid string) (operator.Key, error) {
	if owner != k.key.Operator || kid != k.key.KID {
		return operator.Key{}, operator.ErrClientInvalid
	}
	return k.key, nil
}

func (k *keyExchanges) RecordAssertionToken(_ context.Context, a operator.VerifiedAssertion, t *operator.Token) error {
	k.admitted = append(k.admitted, a)
	t.IssuedAt, t.ExpiresAt = a.IssuedAt, a.IssuedAt.Add(operator.TokenTTL)
	k.recorded = append(k.recorded, t)
	return nil
}

// assertionExchanging answers a service over one live key of client,
// with the token endpoint configured as endpoint.
func assertionExchanging(t *testing.T, endpoint string) (operator.Service, *keyExchanges) {
	t.Helper()
	private, _ := keys(t)
	store := &keyExchanges{exchanges: newExchanges(), key: recorded(t, client, private)}
	svc := operator.NewService(newRecords(), newTokens(), store, &signer{},
		operator.Config{TokenEndpoint: endpoint})
	return svc, store
}

// assertionAt signs a valid assertion of client issued at now with jti.
func assertionAt(t *testing.T, now time.Time, jti string) string {
	t.Helper()
	private, _ := keys(t)
	c := claims(now)
	c["jti"] = jti
	return signRaw(t, private, headerFor(t, private), encode(t, c))
}

func TestService_ExchangeAssertionAnswersATokenForTheKeysOperator(t *testing.T) {
	svc, store := assertionExchanging(t, endpoint)
	now := time.Now()

	token, err := svc.ExchangeAssertion(context.Background(), testSource, client, assertionAt(t, now, "jti-0000000000000000000001"), nil)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token.Value, operator.TokenPrefix))
	assert.Equal(t, client, token.Operator)
	assert.Nil(t, token.Audience)
	require.Len(t, store.admitted, 1)
	assert.Equal(t, "jti-0000000000000000000001", store.admitted[0].JTI)
	assert.Equal(t, store.key.ID, store.admitted[0].KeyID, "the token rests on the key that verified it")
}

// TestService_ExchangeAssertionRefusesWithoutAnEndpoint is the profile
// unconfigured (ADR-0102): nothing is verified or recorded, and the
// refusal is unavailable rather than the caller's. The same assertion
// on a configured service is the control.
func TestService_ExchangeAssertionRefusesWithoutAnEndpoint(t *testing.T) {
	assertion := assertionAt(t, time.Now(), "jti-0000000000000000000002")

	svc, store := assertionExchanging(t, "")
	_, err := svc.ExchangeAssertion(context.Background(), testSource, client, assertion, nil)
	assert.ErrorIs(t, err, operator.ErrAssertionProfileUnavailable)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
	assert.Empty(t, store.admitted)

	configured, _ := assertionExchanging(t, endpoint)
	_, err = configured.ExchangeAssertion(context.Background(), testSource, client, assertion, nil)
	assert.NoError(t, err)
}

// TestService_ExchangeAssertionHoldsTheAudienceToAnotherOperatorOfTheOrganization
// refuses an audience in another organization, of another kind, or
// the caller itself; a peer of the same organization is the control.
func TestService_ExchangeAssertionHoldsTheAudienceToAnotherOperatorOfTheOrganization(t *testing.T) {
	svc, store := assertionExchanging(t, endpoint)
	now := time.Now()
	for i, audience := range []grn.GRN{
		operatorGRN("globex", "control"),
		agentGRN("acme", "a1"),
		client,
	} {
		_, err := svc.ExchangeAssertion(context.Background(), testSource, client,
			assertionAt(t, now, "jti-refused-0000000000000"+strconv.Itoa(i)), &audience)
		assert.ErrorIs(t, err, operator.ErrAudienceInvalid, audience.String())
	}
	assert.Empty(t, store.admitted, "nothing was admitted for a refused audience")

	peer := operatorGRN("acme", "control")
	token, err := svc.ExchangeAssertion(context.Background(), testSource, client, assertionAt(t, now, "jti-peer-00000000000000000"), &peer)
	require.NoError(t, err)
	require.NotNil(t, token.Audience)
	assert.Equal(t, peer, *token.Audience)
}

// TestService_ExchangeAssertionIsBoundedPerClient holds the per-process
// bound (ADR-0102): the client's eleventh verified exchange inside the
// window is refused as exhausted, with a retry-after, and records
// nothing; another client in the same window is the control. Assertions
// that fail to verify spend none of the client's budget first, so a
// stranger knowing the GRN cannot lock the operator out.
func TestService_ExchangeAssertionIsBoundedPerClient(t *testing.T) {
	svc, store := assertionExchanging(t, endpoint)
	now := time.Now()
	_, other := keys(t)
	for i := range 2 * operator.ExchangesPerClient {
		c := claims(now)
		c["jti"] = "jti-stranger-0000000000" + strconv.Itoa(100+i)
		_, err := svc.ExchangeAssertion(context.Background(), testSource, client,
			signRaw(t, other, headerFor(t, other), encode(t, c)), nil)
		require.ErrorIs(t, err, operator.ErrClientInvalid, "a stranger's assertion is refused, not counted")
	}
	for i := range operator.ExchangesPerClient {
		_, err := svc.ExchangeAssertion(context.Background(), testSource, client,
			assertionAt(t, now, "jti-bounded-00000000000"+strconv.Itoa(100+i)), nil)
		require.NoError(t, err)
	}
	_, err := svc.ExchangeAssertion(context.Background(), testSource, client, assertionAt(t, now, "jti-bounded-over-00000000"), nil)
	var exceeded *operator.Exceeded
	require.ErrorAs(t, err, &exceeded)
	assert.True(t, errorx.IsKind(err, errorx.KindResourceExhausted))
	assert.Positive(t, exceeded.RetryAfterSeconds())
	assert.Len(t, store.admitted, operator.ExchangesPerClient, "the refused exchange recorded nothing")

	_, err = svc.ExchangeAssertion(context.Background(), testSource, stranger, "not.an.assertion", nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "another client is not bounded by this one's count")
}

// TestService_ExchangeAssertionBoundsUnverifiedTrafficPerAddress is the
// PM's review of #1360, blocking item 2: a flood of unverified
// assertions from one address is refused as exhausted once it passes
// ExchangesPerSource, before anything is read, and a valid client
// arriving from another address in the same window is still served.
func TestService_ExchangeAssertionBoundsUnverifiedTrafficPerAddress(t *testing.T) {
	svc, store := assertionExchanging(t, endpoint)
	const flooder, operatorHost = "198.51.100.7", "203.0.113.20"
	for range operator.ExchangesPerSource {
		_, err := svc.ExchangeAssertion(context.Background(), flooder, client, "not.an.assertion", nil)
		require.ErrorIs(t, err, operator.ErrClientInvalid)
	}
	_, err := svc.ExchangeAssertion(context.Background(), flooder, client, "not.an.assertion", nil)
	var exceeded *operator.Exceeded
	require.ErrorAs(t, err, &exceeded, "the flooding address is refused past its bound")

	token, err := svc.ExchangeAssertion(context.Background(), operatorHost, client,
		assertionAt(t, time.Now(), "jti-another-address-000000"), nil)
	require.NoError(t, err, "a valid client from another address is served")
	assert.Equal(t, client, token.Operator)
	assert.Len(t, store.admitted, 1)
}
