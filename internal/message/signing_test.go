package message_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"sort"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/message"
)

// keyring is a message.SigningKeyStore on a clock the test moves. It
// records the lead of every key it was asked to store, which is what
// says whether a key was announced ahead of signing or signs at once.
type keyring struct {
	mu    sync.Mutex
	now   time.Time
	keys  []*message.SigningKey
	leads []time.Duration
}

func (r *keyring) SigningKeys(context.Context) ([]*message.SigningKey, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*message.SigningKey(nil), r.keys...), r.now, nil
}

func (r *keyring) RotateSigningKeys(_ context.Context, prepare func([]*message.SigningKey, time.Time) (*message.NewSigningKey, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, err := prepare(append([]*message.SigningKey(nil), r.keys...), r.now)
	if err != nil || key == nil {
		return err
	}
	r.leads = append(r.leads, key.Lead)
	r.keys = append(r.keys, &message.SigningKey{
		ID: key.ID, PublicKey: key.PublicKey, SealedPrivateKey: key.SealedPrivateKey, SignsFrom: r.now.Add(key.Lead),
	})
	sort.SliceStable(r.keys, func(i, j int) bool { return r.keys[i].SignsFrom.Before(r.keys[j].SignsFrom) })
	return nil
}

func (r *keyring) WithdrawSigningKey(_ context.Context, id message.KeyID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys {
		if k.ID == id {
			at := r.now
			k.WithdrawnAt = &at
			return nil
		}
	}
	return message.ErrNoSuchSigningKey
}

func (r *keyring) at(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = t
}

// plainSealer seals nothing; what is sealed is the real sealer's
// concern, and the rotation policy is what these tests read.
type plainSealer struct{}

func (plainSealer) Seal(_ context.Context, _, plaintext []byte) ([]byte, error) {
	return plaintext, nil
}
func (plainSealer) Open(_ context.Context, _, sealed []byte) ([]byte, error) { return sealed, nil }

var epoch = time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

func newSigner(t *testing.T, ring *keyring) message.Keyring {
	t.Helper()
	s, err := message.NewKeyring(ring, plainSealer{}, "https://garam.example", message.Config{})
	require.NoError(t, err)
	return s
}

func published(t *testing.T, s message.Keyring) []message.KeyID {
	t.Helper()
	keys, err := s.PublishedKeys(context.Background())
	require.NoError(t, err)
	ids := make([]message.KeyID, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	return ids
}

// TestSigner_TheFirstKeySignsTheMomentItIsCreated holds the one case
// with no lead: a deployment with no key yet has no earlier set its
// first key could be missing from, so the key is created to sign at
// once and published the same moment.
func TestSigner_TheFirstKeySignsTheMomentItIsCreated(t *testing.T) {
	ring := &keyring{now: epoch}
	s := newSigner(t, ring)

	ids := published(t, s)

	require.Len(t, ids, 1)
	assert.Equal(t, []time.Duration{0}, ring.leads)
	assert.Equal(t, epoch, ring.keys[0].SignsFrom)
}

// TestSigner_AnnouncesTheNextKeyTwoHoursBeforeItSigns holds the lead a
// verifier holding the set as a file relies on (ADR-0101): the next key
// is created SigningKeyLead before the current one has signed for
// SigningKeyLifetime, published from then, and only one is announced
// however often the set is read.
func TestSigner_AnnouncesTheNextKeyTwoHoursBeforeItSigns(t *testing.T) {
	ring := &keyring{now: epoch}
	s := newSigner(t, ring)
	first := published(t, s)
	require.Len(t, first, 1)

	due := epoch.Add(message.SigningKeyLifetime - message.SigningKeyLead)

	// The control: a second before it is due, nothing is announced.
	ring.at(due.Add(-time.Second))
	assert.Equal(t, first, published(t, s))
	assert.Len(t, ring.leads, 1, "no key was announced before it was due")

	ring.at(due)
	ids := published(t, s)
	require.Len(t, ring.leads, 2, "the next key was announced when it was due")
	assert.Equal(t, message.SigningKeyLead, ring.leads[1])
	assert.Equal(t, 2*time.Hour, message.SigningKeyLead, "the lead is the two hours ADR-0101 promises")
	next := ring.keys[1]
	assert.Equal(t, due.Add(message.SigningKeyLead), next.SignsFrom)
	assert.Equal(t, append(first, next.ID), ids, "the next key is published from the moment it is announced")

	_ = published(t, s)
	assert.Len(t, ring.leads, 2, "one key is announced at a time")
}

// TestSigner_KeepsAStoppedKeyPublishedWhileItsSignaturesCanBeValid holds
// how long a key that stopped signing stays in the set: until the last
// signature it made has expired, plus the skew allowance, plus one
// cache lifetime.
func TestSigner_KeepsAStoppedKeyPublishedWhileItsSignaturesCanBeValid(t *testing.T) {
	ring := &keyring{now: epoch}
	s := newSigner(t, ring)
	first := published(t, s)[0]
	ring.at(epoch.Add(message.SigningKeyLifetime - message.SigningKeyLead))
	_ = published(t, s)
	require.Len(t, ring.keys, 2)
	second := ring.keys[1]

	stopped := second.SignsFrom
	gone := stopped.Add(message.DefaultLeaseCeiling + message.SkewAllowance + message.KeySetMaxAge)

	ring.at(gone.Add(-time.Second))
	assert.Equal(t, []message.KeyID{first, second.ID}, published(t, s), "still published while a signature can be valid")

	ring.at(gone)
	assert.Equal(t, []message.KeyID{second.ID}, published(t, s), "gone once none can be")
}

// TestSigner_ARetiredKeyLeavesTheSetAtOnceAndIsReplacedAtOnce holds
// what retiring a compromised key does: it is published no longer, a
// replacement signs from that moment, and the key it had replaced does
// not come back to sign in its place.
func TestSigner_ARetiredKeyLeavesTheSetAtOnceAndIsReplacedAtOnce(t *testing.T) {
	ring := &keyring{now: epoch}
	s := newSigner(t, ring)
	first := published(t, s)[0]
	ring.at(epoch.Add(message.SigningKeyLifetime))
	_ = published(t, s)
	require.Len(t, ring.keys, 2)
	current := ring.keys[1].ID
	ring.at(epoch.Add(message.SigningKeyLifetime + 24*time.Hour))
	require.Equal(t, []message.KeyID{current}, published(t, s), "the setup left the current key alone in the set")

	require.NoError(t, ring.WithdrawSigningKey(context.Background(), current))
	ids := published(t, s)

	require.Len(t, ring.leads, 3)
	assert.Equal(t, time.Duration(0), ring.leads[2], "the replacement signs at once")
	assert.Equal(t, []message.KeyID{ring.keys[2].ID}, ids)
	assert.NotContains(t, ids, current)
	assert.NotContains(t, ids, first)
}

// TestThumbprint_AgreesWithAnIndependentJOSELibrary holds the kid to
// RFC 7638 by computing it a second way, with go-jose.
func TestThumbprint_AgreesWithAnIndependentJOSELibrary(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	point, err := key.PublicKey.Bytes()
	require.NoError(t, err)

	ours, err := message.Thumbprint(point)
	require.NoError(t, err)
	theirs, err := (&jose.JSONWebKey{Key: &key.PublicKey}).Thumbprint(crypto.SHA256)
	require.NoError(t, err)

	assert.Equal(t, base64.RawURLEncoding.EncodeToString(theirs), string(ours))
}

// TestParseIssuer_AcceptsAnHTTPSOriginAlone holds the issuer to the one
// spelling a verifier compares byte for byte.
func TestParseIssuer_AcceptsAnHTTPSOriginAlone(t *testing.T) {
	for _, ok := range []string{"https://garam.example", "https://garam.example:8443"} {
		issuer, err := message.ParseIssuer(ok)
		require.NoError(t, err, ok)
		assert.Equal(t, message.Issuer(ok), issuer)
	}
	tests := []struct{ name, issuer string }{
		{"empty", ""},
		{"plain http", "http://garam.example"},
		{"trailing slash", "https://garam.example/"},
		{"path", "https://garam.example/garam"},
		{"query", "https://garam.example?x=1"},
		{"empty query", "https://garam.example?"},
		{"fragment", "https://garam.example#x"},
		{"empty fragment", "https://garam.example#"},
		{"user info", "https://u@garam.example"},
		{"no scheme", "garam.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := message.ParseIssuer(tt.issuer)
			assert.Error(t, err)
		})
	}
}
