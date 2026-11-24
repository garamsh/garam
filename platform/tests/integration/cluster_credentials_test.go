package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	clusterpg "github.com/garamsh/garam/platform/internal/cluster/postgres"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// clusterKeys are drawn once: a 3072-bit key is slow to draw.
var (
	clusterKeysOnce sync.Once
	clusterKeys     [4]cluster.PublicKey
)

func connectorKeys(t *testing.T) [4]cluster.PublicKey {
	t.Helper()
	clusterKeysOnce.Do(func() {
		for i := range clusterKeys {
			k, err := rsa.GenerateKey(rand.Reader, cluster.MinKeyBits)
			if err != nil {
				panic(err)
			}
			raw, err := json.Marshal(jose.JSONWebKey{Key: &k.PublicKey, Algorithm: "RS256", Use: "sig"})
			if err != nil {
				panic(err)
			}
			if clusterKeys[i], err = cluster.ParsePublicJWK(raw); err != nil {
				panic(err)
			}
		}
	})
	return clusterKeys
}

// registeredCluster records a cluster of a new organization and binds
// key to it, as a registration does.
func registeredCluster(t *testing.T, p *pgxpool.Pool, s *clusterpg.Store, key cluster.PublicKey) grn.GRN {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	c, err := cluster.New(orgRootGRN("acme"), scopedGRN("acme", "user", "ada"), cluster.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.Create(ctx, c)
	require.NoError(t, err)
	require.NoError(t, s.BindKey(ctx, c.GRN, key))
	return c.GRN
}

// signed is a verified JWS's claims as the store spends them.
func signed(t *testing.T, g grn.GRN, keyID int64, jti string) operator.VerifiedAssertion {
	t.Helper()
	now := time.Now()
	return operator.VerifiedAssertion{KeyID: keyID, Operator: g, JTI: jti, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
}

func liveKey(t *testing.T, s *clusterpg.Store, g grn.GRN, k cluster.PublicKey) cluster.Key {
	t.Helper()
	key, err := s.VerificationKey(context.Background(), g, k.KID())
	require.NoError(t, err)
	return key
}

// TestClusterCredentials_ARegistrationTokenIsSpentOnceAndOnlyBeforeItExpires
// holds REG-3 to REG-5 at the store: presenting a token deletes it,
// whatever follows, and an expired one is refused as a spent one is.
func TestClusterCredentials_ARegistrationTokenIsSpentOnceAndOnlyBeforeItExpires(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	g := registeredCluster(t, p, s, connectorKeys(t)[0])

	_, err := s.IssueRegistrationToken(ctx, g, "live-hash", time.Hour)
	require.NoError(t, err)
	spent, err := s.SpendRegistrationToken(ctx, "live-hash")
	require.NoError(t, err)
	assert.Equal(t, g, spent)
	_, err = s.SpendRegistrationToken(ctx, "live-hash")
	assert.ErrorIs(t, err, cluster.ErrRegistrationRefused, "a second presentation")

	_, err = s.IssueRegistrationToken(ctx, g, "expired-hash", -time.Second)
	require.NoError(t, err)
	_, err = s.SpendRegistrationToken(ctx, "expired-hash")
	assert.ErrorIs(t, err, cluster.ErrRegistrationRefused, "an expired token")
	_, err = s.SpendRegistrationToken(ctx, "unknown-hash")
	assert.ErrorIs(t, err, cluster.ErrRegistrationRefused, "an unknown token")
}

// TestClusterCredentials_RegistrationLeavesTheBoundKeyTheOnlyLiveOne
// holds REG-6: every key held before is withdrawn, with its tokens.
func TestClusterCredentials_RegistrationLeavesTheBoundKeyTheOnlyLiveOne(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	keys := connectorKeys(t)
	g := registeredCluster(t, p, s, keys[0])
	first := liveKey(t, s, g, keys[0])
	token := &cluster.Token{Cluster: g, Value: "garam_ct_first"}
	require.NoError(t, s.RecordAssertionToken(ctx, signed(t, g, first.ID, "jti-first"), token))
	_, err := s.ResolveToken(ctx, cluster.HashSecret(token.Value))
	require.NoError(t, err, "the control: the token resolves while its key is live")

	require.NoError(t, s.BindKey(ctx, g, keys[1]))
	_, err = s.VerificationKey(ctx, g, keys[0].KID())
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the key held before")
	_, err = s.ResolveToken(ctx, cluster.HashSecret(token.Value))
	assert.ErrorIs(t, err, cluster.ErrTokenInvalid, "its token")
	liveKey(t, s, g, keys[1])
}

// TestClusterCredentials_ARotationIsAnsweredOnceAndRepeatedAsItWas
// holds ROT-3 and ROT-4 at the store: the successor is live beside a
// predecessor retiring at least 24 hours away, a repeat is answered
// as the first was, a third key is refused while two are live, and a
// rotation to any key held but a rotated live successor is refused.
func TestClusterCredentials_ARotationIsAnsweredOnceAndRepeatedAsItWas(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	keys := connectorKeys(t)
	g := registeredCluster(t, p, s, keys[0])
	registered := liveKey(t, s, g, keys[0])

	_, err := s.Rotate(ctx, g, registered.ID, keys[0], []operator.VerifiedAssertion{signed(t, g, registered.ID, "self")})
	assert.ErrorIs(t, err, cluster.ErrKeyHeld, "the key registration bound")

	first, err := s.Rotate(ctx, g, registered.ID, keys[1],
		[]operator.VerifiedAssertion{signed(t, g, registered.ID, "a1"), signed(t, g, registered.ID, "p1")})
	require.NoError(t, err)
	assert.Equal(t, keys[1].KID(), first.KID)
	assert.WithinDuration(t, time.Now().Add(cluster.RolloverWindow), first.PredecessorRetiresAt, time.Minute)
	assert.GreaterOrEqual(t, cluster.RolloverWindow, 24*time.Hour)
	liveKey(t, s, g, keys[0])
	successor := liveKey(t, s, g, keys[1])

	repeat, err := s.Rotate(ctx, g, successor.ID, keys[1],
		[]operator.VerifiedAssertion{signed(t, g, successor.ID, "a2"), signed(t, g, successor.ID, "p2")})
	require.NoError(t, err)
	assert.Equal(t, first.KID, repeat.KID)
	assert.True(t, first.PredecessorRetiresAt.Equal(repeat.PredecessorRetiresAt), "the same deadline")

	_, err = s.Rotate(ctx, g, successor.ID, keys[2], []operator.VerifiedAssertion{signed(t, g, successor.ID, "a3")})
	assert.ErrorIs(t, err, cluster.ErrTooManyKeys, "a third key beside two live")

	_, err = s.Rotate(ctx, g, successor.ID, keys[1], []operator.VerifiedAssertion{signed(t, g, successor.ID, "a2")})
	assert.ErrorIs(t, err, cluster.ErrInvalidProof, "a jti spent before")

	require.NoError(t, s.RevokeKey(ctx, g, keys[1].KID()))
	_, err = s.Rotate(ctx, g, registered.ID, keys[1], []operator.VerifiedAssertion{signed(t, g, registered.ID, "a4")})
	assert.ErrorIs(t, err, cluster.ErrKeyHeld, "a rotated key since withdrawn")

	_, err = s.Rotate(ctx, g, successor.ID, keys[3], []operator.VerifiedAssertion{signed(t, g, successor.ID, "a5")})
	assert.ErrorIs(t, err, cluster.ErrInvalidProof, "authorized by a key no longer live")
}

// TestClusterCredentials_RevokingAKeyWithdrawsItsTokens holds ADR-0184
// decision 7: the next request on a revoked key's token is refused, and
// a token of the cluster's other key is the control.
func TestClusterCredentials_RevokingAKeyWithdrawsItsTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	keys := connectorKeys(t)
	g := registeredCluster(t, p, s, keys[0])
	registered := liveKey(t, s, g, keys[0])
	_, err := s.Rotate(ctx, g, registered.ID, keys[1], []operator.VerifiedAssertion{signed(t, g, registered.ID, "a")})
	require.NoError(t, err)
	successor := liveKey(t, s, g, keys[1])
	revoked := &cluster.Token{Cluster: g, Value: "garam_ct_revoked"}
	kept := &cluster.Token{Cluster: g, Value: "garam_ct_kept"}
	require.NoError(t, s.RecordAssertionToken(ctx, signed(t, g, registered.ID, "x1"), revoked))
	require.NoError(t, s.RecordAssertionToken(ctx, signed(t, g, successor.ID, "x2"), kept))

	require.NoError(t, s.RevokeKey(ctx, g, keys[0].KID()))
	_, err = s.ResolveToken(ctx, cluster.HashSecret(revoked.Value))
	assert.ErrorIs(t, err, cluster.ErrTokenInvalid)
	_, err = s.ResolveToken(ctx, cluster.HashSecret(kept.Value))
	assert.NoError(t, err)
	assert.NoError(t, s.RevokeKey(ctx, g, keys[0].KID()), "a key already revoked stays so")
	assert.ErrorIs(t, s.RevokeKey(ctx, g, keys[2].KID()), cluster.ErrKeyNotFound, "a key never held")

	require.NoError(t, s.Delete(ctx, g))
	_, err = s.ResolveToken(ctx, cluster.HashSecret(kept.Value))
	assert.ErrorIs(t, err, cluster.ErrTokenInvalid, "a deleted record's token")
}
