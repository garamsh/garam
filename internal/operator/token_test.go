package operator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// exchanges is the TokenStore in memory: credentials by hash, and the
// tokens recorded under them.
type exchanges struct {
	credentials map[string]operator.CredentialHolder
	recorded    []*operator.Token
}

func newExchanges() *exchanges {
	return &exchanges{credentials: map[string]operator.CredentialHolder{}}
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

func (e *exchanges) ResolveToken(context.Context, string) (operator.TokenHolder, error) {
	return operator.TokenHolder{}, operator.ErrTokenInvalid
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
