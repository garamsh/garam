package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// minted is the AccessTokenStore in memory: it records what it was
// asked to mint, at the epoch it was told the agent is at, and refuses
// with err where one is set.
type minted struct {
	tokens  []*AccessToken
	callers []grn.GRN
	epoch   int
	err     error
}

func (m *minted) MintAccessToken(_ context.Context, caller grn.GRN, t *AccessToken) error {
	if m.err != nil {
		return m.err
	}
	t.Assignee, t.Epoch = caller, m.epoch
	m.tokens = append(m.tokens, t)
	m.callers = append(m.callers, caller)
	return nil
}

func (m *minted) ResolveAccessToken(context.Context, string) (AccessTokenHolder, error) {
	return AccessTokenHolder{}, ErrAccessTokenInvalid
}

// registered is RegisteredOperators in memory: the operators an
// organization holds a record of.
type registered map[grn.GRN]bool

func (r registered) Registered(_ context.Context, operator grn.GRN) error {
	if !r[operator] {
		return errorx.New(errorx.KindNotFound, "operator not found")
	}
	return nil
}

// hosted is the one operator the registry below holds besides the
// controller: the hosted control service a token may be shown to.
var hosted = grn.New("acme", grn.DefaultProject, ServiceTypeOperator, "control")

func minting(t *testing.T, store *minted, lifetime time.Duration) (Service, time.Time) {
	t.Helper()
	at := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	svc, err := NewService(nil, store, registered{hosted: true}, nil, Config{TokenLifetime: lifetime,
		Backdate: time.Minute, ServerRootPEM: []byte("root"), Now: func() time.Time { return at }})
	require.NoError(t, err)
	return svc, at
}

func TestService_MintAccessTokenAnswersATokenForTheAgentAtItsEpoch(t *testing.T) {
	store := &minted{epoch: 4}
	svc, at := minting(t, store, 0)
	controller := grn.New("acme", grn.DefaultProject, ServiceTypeOperator, "controller")
	bot := grn.New("acme", grn.DefaultProject, ServiceType, "0a1b2c3d4e5f6071")

	token, err := svc.MintAccessToken(context.Background(), controller, bot, nil)
	require.NoError(t, err)

	assert.True(t, WellFormedAccessToken(token.Value))
	assert.Equal(t, bot, token.Agent)
	assert.Equal(t, controller, token.Assignee)
	assert.Equal(t, 4, token.Epoch)
	assert.Equal(t, at.Add(DefaultAccessTokenLifetime), token.ExpiresAt, "the expiry is measured from now")
	assert.Equal(t, at.Add(-time.Minute), token.IssuedAt,
		"the start is backdated, so a database whose clock trails this one finds it current")
	assert.Equal(t, []byte("root"), token.ServerRootPEM)
	assert.True(t, strings.HasPrefix(token.Serial(), "t"))
	assert.Len(t, token.Serial(), 33)
	require.Len(t, store.tokens, 1)
	assert.Equal(t, token.Hash(), store.tokens[0].Hash())
}

// TestService_MintAccessTokenRefusesWhatTheStoreIsNotAsked holds the
// refusals the service makes before the store: a caller that is not an
// operator, and an audience that is not another operator of the
// agent's organization. The accepted audience is the control.
func TestService_MintAccessTokenRefusesWhatTheStoreIsNotAsked(t *testing.T) {
	store := &minted{epoch: 1}
	svc, _ := minting(t, store, 0)
	controller := grn.New("acme", grn.DefaultProject, ServiceTypeOperator, "controller")
	bot := grn.New("acme", grn.DefaultProject, ServiceType, "0a1b2c3d4e5f6071")

	_, err := svc.MintAccessToken(context.Background(), bot, bot, nil)
	assert.ErrorIs(t, err, ErrNotAnOperator, "an agent mints no token")

	_, err = svc.MintAccessToken(context.Background(), controller, bot, &hosted)
	require.NoError(t, err)
	for name, audience := range map[string]grn.GRN{
		"the caller":      controller,
		"another org":     grn.New("globex", grn.DefaultProject, ServiceTypeOperator, "control"),
		"not an operator": bot,
		"not registered":  grn.New("acme", grn.DefaultProject, ServiceTypeOperator, "x1"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.MintAccessToken(context.Background(), controller, bot, &audience)
			assert.ErrorIs(t, err, ErrAccessTokenAudience)
		})
	}
	assert.Len(t, store.tokens, 1, "only the accepted audience reached the store")

	store.err = ErrNotAssigned
	_, err = svc.MintAccessToken(context.Background(), controller, bot, nil)
	assert.ErrorIs(t, err, ErrNotAssigned, "the store's refusal is the answer")
}

func TestNewService_BoundsTheTokenLifetime(t *testing.T) {
	for _, lifetime := range []time.Duration{0, MinAccessTokenLifetime, MaxAccessTokenLifetime} {
		_, err := NewService(nil, &minted{}, nil, nil, Config{TokenLifetime: lifetime})
		assert.NoError(t, err, lifetime)
	}
	for _, lifetime := range []time.Duration{MinAccessTokenLifetime - time.Second, MaxAccessTokenLifetime + time.Second} {
		_, err := NewService(nil, &minted{}, nil, nil, Config{TokenLifetime: lifetime})
		assert.ErrorIs(t, err, ErrAccessTokenLifetime, lifetime)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	}
}

func TestWellFormedAccessToken_IsThePrefixAnd256BitsOfLowercaseHex(t *testing.T) {
	assert.True(t, WellFormedAccessToken(AccessTokenPrefix+strings.Repeat("0a", 32)))
	for _, value := range []string{
		"garam_ot_" + strings.Repeat("0", 64),
		AccessTokenPrefix + strings.Repeat("0", 63),
		AccessTokenPrefix + strings.Repeat("A", 64),
		AccessTokenPrefix + strings.Repeat("g", 64),
	} {
		assert.False(t, WellFormedAccessToken(value), value)
	}
}
