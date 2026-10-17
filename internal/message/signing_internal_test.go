package message

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
)

// These tests are internal because they sign: a Claim is built only by
// the store that read its row, and signing one is what they observe.

// clockedKeys is a SigningKeyStore on a clock the test moves.
type clockedKeys struct {
	now   time.Time
	keys  []*SigningKey
	leads []time.Duration
}

func (c *clockedKeys) SigningKeys(context.Context) ([]*SigningKey, time.Time, error) {
	return append([]*SigningKey(nil), c.keys...), c.now, nil
}

func (c *clockedKeys) RotateSigningKeys(_ context.Context, prepare func([]*SigningKey, time.Time) (*NewSigningKey, error)) error {
	key, err := prepare(append([]*SigningKey(nil), c.keys...), c.now)
	if err != nil || key == nil {
		return err
	}
	c.leads = append(c.leads, key.Lead)
	c.keys = append(c.keys, &SigningKey{ID: key.ID, PublicKey: key.PublicKey, Held: key.Held, SignsFrom: c.now.Add(key.Lead)})
	sort.SliceStable(c.keys, func(i, j int) bool { return c.keys[i].SignsFrom.Before(c.keys[j].SignsFrom) })
	return nil
}

func (c *clockedKeys) WithdrawSigningKey(_ context.Context, id KeyID) error {
	for _, k := range c.keys {
		if k.ID == id {
			at := c.now
			k.WithdrawnAt = &at
			return nil
		}
	}
	return ErrNoSuchSigningKey
}

type openSealer struct{}

func (openSealer) Seal(_ context.Context, _, plaintext []byte) ([]byte, error) { return plaintext, nil }
func (openSealer) Open(_ context.Context, _, sealed []byte) ([]byte, error)    { return sealed, nil }

var start = time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

// signedBy signs a claim taken at the store's clock and answers the
// protected header's members, which is where the signing key is named.
func signedBy(t *testing.T, ring Keyring, keys *clockedKeys) map[string]string {
	t.Helper()
	signer, err := ring.Signer(context.Background())
	require.NoError(t, err)
	recipient, err := grn.Parse("grn:acme:default:agent:0a1b2c3d4e5f6071")
	require.NoError(t, err)
	sender, err := grn.Parse("grn:acme:default:agent:9f8e7d6c5b4a3021")
	require.NoError(t, err)
	m, err := New(recipient, sender, "key-1", "hello")
	require.NoError(t, err)
	signed, err := signer.Sign(context.Background(), &Claim{message: m, id: "claim-1", claimedAt: keys.now, expiresAt: keys.now.Add(time.Minute)})
	require.NoError(t, err)
	encoded, _, ok := strings.Cut(signed.Signature, ".")
	require.True(t, ok)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var header map[string]string
	require.NoError(t, json.Unmarshal(raw, &header))
	return header
}

func newRing(t *testing.T, keys *clockedKeys) Keyring {
	t.Helper()
	ring, err := NewKeyring(keys, "https://garam.example", Config{}, NewFileCustodian(openSealer{}))
	require.NoError(t, err)
	return ring
}

// TestSigner_TheHeaderHoldsAlgKidAndTypAndNothingElse holds the header
// ADR-0101 fixes: a verifier takes no key, and no place to fetch one,
// from the message, so no member beside these three is there to take.
func TestSigner_TheHeaderHoldsAlgKidAndTypAndNothingElse(t *testing.T) {
	keys := &clockedKeys{now: start}
	header := signedBy(t, newRing(t, keys), keys)

	require.Len(t, keys.keys, 1)
	assert.Equal(t, map[string]string{"alg": "ES256", "kid": string(keys.keys[0].ID), "typ": "agent-message+jwt"}, header)
}

// TestSigner_SignsUnderTheKeyThatHasBegunAcrossARotation holds which key
// signs on either side of a rotation: the current key until the second
// its successor begins, and the successor from that second.
func TestSigner_SignsUnderTheKeyThatHasBegunAcrossARotation(t *testing.T) {
	keys := &clockedKeys{now: start}
	ring := newRing(t, keys)
	first := signedBy(t, ring, keys)["kid"]

	keys.now = start.Add(SigningKeyLifetime - SigningKeyLead)
	assert.Equal(t, first, signedBy(t, ring, keys)["kid"], "the announced key does not sign before its lead has passed")
	require.Len(t, keys.keys, 2, "the next key was announced")
	next := keys.keys[1]

	keys.now = next.SignsFrom.Add(-time.Second)
	assert.Equal(t, first, signedBy(t, ring, keys)["kid"])

	keys.now = next.SignsFrom
	assert.Equal(t, string(next.ID), signedBy(t, ring, keys)["kid"])
	assert.Len(t, keys.keys, 2, "the rotation created no further key")
}

// TestSigner_RetiringTheNextKeyLeavesTheCurrentOneSigning holds the
// two-hour promise through a retired announcement: retiring the next key
// before it begins leaves the current key signing, announces a
// replacement with the full lead, and the retired key's start passes
// without anything signing from creation.
func TestSigner_RetiringTheNextKeyLeavesTheCurrentOneSigning(t *testing.T) {
	keys := &clockedKeys{now: start}
	ring := newRing(t, keys)
	current := signedBy(t, ring, keys)["kid"]
	keys.now = start.Add(SigningKeyLifetime - SigningKeyLead)
	signedBy(t, ring, keys)
	require.Len(t, keys.keys, 2)
	retired := keys.keys[1]

	// Retired an hour after it was announced, so its start and its
	// replacement's are an hour apart and each boundary is its own.
	keys.now = keys.now.Add(time.Hour)
	require.NoError(t, keys.WithdrawSigningKey(context.Background(), retired.ID))
	assert.Equal(t, current, signedBy(t, ring, keys)["kid"], "the current key signs on")
	require.Len(t, keys.leads, 3, "a replacement was announced")
	assert.Equal(t, SigningKeyLead, keys.leads[2], "with the full lead")
	replacement := keys.keys[len(keys.keys)-1]
	require.NotEqual(t, retired.ID, replacement.ID)

	require.True(t, replacement.SignsFrom.After(retired.SignsFrom), "the setup left the two starts apart")
	keys.now = retired.SignsFrom
	assert.Equal(t, current, signedBy(t, ring, keys)["kid"], "the retired key's start changes nothing")
	assert.Len(t, keys.leads, 3, "nothing was created to sign at once")

	keys.now = replacement.SignsFrom
	assert.Equal(t, string(replacement.ID), signedBy(t, ring, keys)["kid"])
}
