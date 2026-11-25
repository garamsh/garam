package app_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/operator"
)

const (
	tokenURL    = "https://machine.garam.test/oauth/token"
	rotationURL = "https://machine.garam.test/cluster-connection/v1alpha1/clusters/self/keys"
)

var self = grn.New("acme", app.InstanceType, "5f60710a1b2c3d4e")

// keys are generated once: 3072-bit RSA is slow to draw.
var (
	keysOnce sync.Once
	current  *rsa.PrivateKey
	next     *rsa.PrivateKey
	stranger *rsa.PrivateKey
)

func testKeys(t *testing.T) {
	t.Helper()
	keysOnce.Do(func() {
		for _, k := range []**rsa.PrivateKey{&current, &next, &stranger} {
			var err error
			*k, err = rsa.GenerateKey(rand.Reader, 3072)
			if err != nil {
				panic(err)
			}
		}
	})
}

func kid(t *testing.T, k *rsa.PrivateKey) string {
	t.Helper()
	id, err := operator.Thumbprint(&k.PublicKey)
	require.NoError(t, err)
	return id
}

func publicJWK(t *testing.T, k *rsa.PrivateKey) []byte {
	t.Helper()
	b, err := json.Marshal(jose.JSONWebKey{Key: &k.PublicKey, Algorithm: "RS256", Use: "sig"})
	require.NoError(t, err)
	return b
}

// sign signs claims with k under typ, naming kid in the header.
func sign(t *testing.T, k *rsa.PrivateKey, kidValue, typ string, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: k},
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", kidValue))
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signed, err := signer.Sign(payload)
	require.NoError(t, err)
	compact, err := signed.CompactSerialize()
	require.NoError(t, err)
	return compact
}

var jtiCounter int

func requestClaims(aud string, extra map[string]any) map[string]any {
	jtiCounter++
	now := time.Now()
	c := map[string]any{"iss": self.String(), "sub": self.String(), "aud": aud,
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
		"jti": fmt.Sprintf("dGVzdC1qdGktMTI4LWJpdHM%012d", jtiCounter)}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

// credentials is a CredentialStore that records what the service asked
// of it and holds one live key, current's.
type credentials struct {
	app.CredentialStore
	t        *testing.T
	spent    []string
	bound    []app.PublicKey
	rotated  []int64
	recorded []*app.Token
	// bindErr is what BindKey answers.
	bindErr error
}

func (c *credentials) SpendRegistrationToken(_ context.Context, hash string) (grn.GRN, error) {
	c.spent = append(c.spent, hash)
	if hash != app.HashSecret(liveToken) {
		return grn.GRN{}, app.ErrRegistrationRefused
	}
	return self, nil
}

func (c *credentials) BindKey(_ context.Context, g grn.GRN, key app.PublicKey) error {
	c.bound = append(c.bound, key)
	return c.bindErr
}

func (c *credentials) VerificationKey(_ context.Context, g grn.GRN, kidValue string) (app.Key, error) {
	if g != self || kidValue != kid(c.t, current) {
		return app.Key{}, operator.ErrClientInvalid
	}
	return app.Key{ID: 7, Instance: self, KID: kidValue, Public: &current.PublicKey, State: app.KeyStateActive}, nil
}

func (c *credentials) Rotate(_ context.Context, _ grn.GRN, authorizedBy int64, successor app.PublicKey, signed []operator.VerifiedAssertion) (app.Rotation, error) {
	c.rotated = append(c.rotated, authorizedBy)
	return app.Rotation{KID: successor.KID()}, nil
}

func (c *credentials) RecordAssertionToken(_ context.Context, _ operator.VerifiedAssertion, tok *app.Token) error {
	c.recorded = append(c.recorded, tok)
	return nil
}

var liveToken = "garam_crt_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newService(t *testing.T) (app.Service, *credentials) {
	testKeys(t)
	store := &credentials{t: t}
	return app.NewService(store, app.Config{TokenEndpoint: tokenURL, RotationURL: rotationURL}), store
}

// TestRegister_SpendsTheTokenBeforeTheKeyIsRead holds REG-3: a token
// presented with a key garam refuses is spent all the same. The
// accepted registration is the control: it spends and binds.
func TestRegister_SpendsTheTokenBeforeTheKeyIsRead(t *testing.T) {
	svc, store := newService(t)
	small, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), "192.0.2.1", liveToken, publicJWK(t, small))
	assert.ErrorIs(t, err, app.ErrInvalidKey)
	assert.Equal(t, []string{app.HashSecret(liveToken)}, store.spent, "the token was spent")
	assert.Empty(t, store.bound)

	g, err := svc.Register(context.Background(), "192.0.2.1", liveToken, publicJWK(t, current))
	require.NoError(t, err)
	assert.Equal(t, self, g)
	require.Len(t, store.bound, 1)
	assert.Equal(t, kid(t, current), store.bound[0].KID())
}

// TestRegister_RefusesWhatNoMintProducedWithoutReading holds that a
// value no mint could have drawn is refused as an unknown token is,
// and reads nothing.
func TestRegister_RefusesWhatNoMintProducedWithoutReading(t *testing.T) {
	svc, store := newService(t)
	for _, token := range []string{"", "garam_crt_short", "garam_ct_" + liveToken[len("garam_crt_"):],
		"garam_crt_" + "0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef"} {
		_, err := svc.Register(context.Background(), "192.0.2.1", token, publicJWK(t, current))
		assert.ErrorIs(t, err, app.ErrRegistrationRefused, token)
	}
	assert.Empty(t, store.spent)
}

// TestRegister_ThrottlesBeforeTheTokenIsRead holds REG-8: past the
// bound a request is answered without spending, and another address
// is the control.
func TestRegister_ThrottlesBeforeTheTokenIsRead(t *testing.T) {
	svc, store := newService(t)
	for range app.RegistrationsPerSource {
		_, _ = svc.Register(context.Background(), "192.0.2.1", "garam_crt_bad", nil)
	}
	before := len(store.spent)
	_, err := svc.Register(context.Background(), "192.0.2.1", liveToken, publicJWK(t, current))
	var exceeded *operator.Exceeded
	require.ErrorAs(t, err, &exceeded)
	assert.Len(t, store.spent, before, "a throttled request spends nothing")

	_, err = svc.Register(context.Background(), "192.0.2.2", liveToken, publicJWK(t, current))
	assert.NoError(t, err)
}

// TestRegister_TakesTheConfiguredBound holds that Config.RegistrationLimit
// replaces the default bound: with two, the third request from one
// address is throttled, and the default admits it, the control.
func TestRegister_TakesTheConfiguredBound(t *testing.T) {
	testKeys(t)
	throttled := func(cfg app.Config) bool {
		svc := app.NewService(&credentials{t: t}, cfg)
		for range 2 {
			_, _ = svc.Register(context.Background(), "192.0.2.1", "garam_crt_bad", nil)
		}
		_, err := svc.Register(context.Background(), "192.0.2.1", "garam_crt_bad", nil)
		var exceeded *operator.Exceeded
		return errors.As(err, &exceeded)
	}
	assert.True(t, throttled(app.Config{TokenEndpoint: tokenURL, RegistrationLimit: 2}))
	assert.False(t, throttled(app.Config{TokenEndpoint: tokenURL}), "the control: the default bound is ten")
}

// TestParsePublicJWK_TakesThePublicHalfOfAKeyOf3072BitsOrMore holds
// REG-1 and REG-2. The public half of a 3072-bit key is the control.
func TestParsePublicJWK_TakesThePublicHalfOfAKeyOf3072BitsOrMore(t *testing.T) {
	testKeys(t)
	k, err := app.ParsePublicJWK(publicJWK(t, current))
	require.NoError(t, err)
	assert.Equal(t, kid(t, current), k.KID())

	private, err := json.Marshal(jose.JSONWebKey{Key: current, Algorithm: "RS256"})
	require.NoError(t, err)
	var withPrivate map[string]any
	require.NoError(t, json.Unmarshal(publicJWK(t, current), &withPrivate))
	withPrivate["d"] = "AQAB"
	onlyD, err := json.Marshal(withPrivate)
	require.NoError(t, err)
	small, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var otherAlg map[string]any
	require.NoError(t, json.Unmarshal(publicJWK(t, current), &otherAlg))
	otherAlg["alg"] = "PS256"
	alg, err := json.Marshal(otherAlg)
	require.NoError(t, err)

	for name, raw := range map[string][]byte{
		"the whole private key":         private,
		"a public key carrying d":       onlyD,
		"a 2048-bit key":                publicJWK(t, small),
		"another algorithm":             alg,
		"not a JWK":                     []byte(`"key"`),
		"an elliptic curve key, absent": []byte(`{"kty":"EC"}`),
	} {
		_, err := app.ParsePublicJWK(raw)
		assert.ErrorIs(t, err, app.ErrInvalidKey, name)
	}
}

// TestRotate_NeedsTheCurrentKeysAuthorizationAndTheSuccessorsProof
// holds ROT-1 and ROT-2: each way the two JWSs fail to prove both keys
// is refused before the store is asked, and the rotation both prove is
// the control.
func TestRotate_NeedsTheCurrentKeysAuthorizationAndTheSuccessorsProof(t *testing.T) {
	svc, store := newService(t)
	authorization := func(k *rsa.PrivateKey, typ, aud, jkt string) string {
		return sign(t, k, kid(t, current), typ, requestClaims(aud, map[string]any{"jkt": jkt}))
	}
	proof := func(k *rsa.PrivateKey, typ, aud string) string {
		return sign(t, k, kid(t, next), typ, requestClaims(aud, nil))
	}
	successor := kid(t, next)

	rotation, err := svc.Rotate(context.Background(), self, publicJWK(t, next),
		authorization(current, app.TypeKeyRotation, rotationURL, successor),
		proof(next, app.TypeKeyProof, rotationURL))
	require.NoError(t, err)
	assert.Equal(t, successor, rotation.KID)
	assert.Equal(t, []int64{7}, store.rotated, "authorized by the current key's row")

	cases := map[string]struct{ authorization, proof string }{
		"the bearer token alone": {"", ""},
		"authorization signed by another key": {authorization(stranger, app.TypeKeyRotation, rotationURL, successor),
			proof(next, app.TypeKeyProof, rotationURL)},
		"authorization naming another successor": {authorization(current, app.TypeKeyRotation, rotationURL, kid(t, stranger)),
			proof(next, app.TypeKeyProof, rotationURL)},
		"authorization of another type": {authorization(current, app.TypeKeyProof, rotationURL, successor),
			proof(next, app.TypeKeyProof, rotationURL)},
		"authorization for another audience": {authorization(current, app.TypeKeyRotation, tokenURL, successor),
			proof(next, app.TypeKeyProof, rotationURL)},
		"proof signed by another key": {authorization(current, app.TypeKeyRotation, rotationURL, successor),
			proof(stranger, app.TypeKeyProof, rotationURL)},
		"proof of another type": {authorization(current, app.TypeKeyRotation, rotationURL, successor),
			proof(next, app.TypeKeyRotation, rotationURL)},
		"proof for another audience": {authorization(current, app.TypeKeyRotation, rotationURL, successor),
			proof(next, app.TypeKeyProof, tokenURL)},
	}
	for name, c := range cases {
		_, err := svc.Rotate(context.Background(), self, publicJWK(t, next), c.authorization, c.proof)
		assert.ErrorIs(t, err, app.ErrInvalidProof, name)
	}
	assert.Len(t, store.rotated, 1, "no refused rotation reached the store")
}

// TestRotate_RefusesALifetimePastSixtySeconds holds ROT-1's bound on
// exp. The same request inside the bound is the control.
func TestRotate_RefusesALifetimePastSixtySeconds(t *testing.T) {
	svc, store := newService(t)
	long := requestClaims(rotationURL, map[string]any{"jkt": kid(t, next)})
	long["exp"] = time.Now().Add(61 * time.Second).Unix()
	_, err := svc.Rotate(context.Background(), self, publicJWK(t, next),
		sign(t, current, kid(t, current), app.TypeKeyRotation, long),
		sign(t, next, kid(t, next), app.TypeKeyProof, requestClaims(rotationURL, nil)))
	assert.ErrorIs(t, err, app.ErrInvalidProof)
	assert.Empty(t, store.rotated)

	within := requestClaims(rotationURL, map[string]any{"jkt": kid(t, next)})
	within["exp"] = time.Now().Add(60 * time.Second).Unix()
	_, err = svc.Rotate(context.Background(), self, publicJWK(t, next),
		sign(t, current, kid(t, current), app.TypeKeyRotation, within),
		sign(t, next, kid(t, next), app.TypeKeyProof, requestClaims(rotationURL, nil)))
	require.NoError(t, err)
	assert.Len(t, store.rotated, 1)
}

// TestExchangeAssertion_AnswersAnInstanceAndNoOtherIdentity holds that
// the exchange runs the operator's profile over the instance's keys:
// the instance's own assertion is the control for an operator's GRN.
func TestExchangeAssertion_AnswersAnInstanceAndNoOtherIdentity(t *testing.T) {
	svc, store := newService(t)
	assertion := sign(t, current, kid(t, current), "JWT", requestClaims(tokenURL, nil))
	token, err := svc.ExchangeAssertion(context.Background(), "192.0.2.1", self, assertion)
	require.NoError(t, err)
	assert.True(t, app.WellFormed(token.Value, app.TokenPrefix))
	assert.Len(t, store.recorded, 1)

	_, err = svc.ExchangeAssertion(context.Background(), "192.0.2.1", grn.New("acme", "operator", "one"), assertion)
	assert.ErrorIs(t, err, operator.ErrClientInvalid)
	assert.Len(t, store.recorded, 1)
}

// TestRegister_RefusesARecordDeletedBeforeItsKeyIsBound holds that a
// record gone between the spend and the bind is answered as REG-4
// answers a token naming no record, and not 404, which the route does
// not declare. Another failure of the bind is the control: it is not
// recast.
func TestRegister_RefusesARecordDeletedBeforeItsKeyIsBound(t *testing.T) {
	svc, store := newService(t)
	store.bindErr = app.ErrInstanceNotFound
	_, err := svc.Register(context.Background(), "192.0.2.1", liveToken, publicJWK(t, current))
	assert.ErrorIs(t, err, app.ErrRegistrationRefused)

	unavailable := errors.New("the database is down")
	store.bindErr = unavailable
	_, err = svc.Register(context.Background(), "192.0.2.1", liveToken, publicJWK(t, current))
	assert.ErrorIs(t, err, unavailable)
}

// TestRotate_IsBoundedPerInstanceBeforeAnySignatureIsVerified holds the
// rotation route's bound: past RotationsPerInstance an instance's
// request is answered *operator.Exceeded and reaches no verification,
// and another instance's, the control, is not throttled by it.
func TestRotate_IsBoundedPerInstanceBeforeAnySignatureIsVerified(t *testing.T) {
	svc, store := newService(t)
	for range app.RotationsPerInstance {
		_, err := svc.Rotate(context.Background(), self, nil, "", "")
		require.ErrorIs(t, err, app.ErrInvalidKey)
	}
	_, err := svc.Rotate(context.Background(), self, publicJWK(t, next),
		sign(t, current, kid(t, current), app.TypeKeyRotation, requestClaims(rotationURL, map[string]any{"jkt": kid(t, next)})),
		sign(t, next, kid(t, next), app.TypeKeyProof, requestClaims(rotationURL, nil)))
	var exceeded *operator.Exceeded
	require.ErrorAs(t, err, &exceeded)
	assert.Empty(t, store.rotated, "a throttled rotation reached no store")

	other := grn.New("acme", app.InstanceType, "0000000000000001")
	_, err = svc.Rotate(context.Background(), other, nil, "", "")
	assert.ErrorIs(t, err, app.ErrInvalidKey, "the control: another instance's bound is its own")
}

// TestRotate_AnswersAListenerServingNoRotationAsGaramsFailure holds that
// a listener whose token URL names no rotation route answers that it
// is unavailable, not that the instance's proof is wrong. A listener
// serving the route, the control, reaches the proof.
func TestRotate_AnswersAListenerServingNoRotationAsGaramsFailure(t *testing.T) {
	testKeys(t)
	unserved := app.NewService(&credentials{t: t}, app.Config{TokenEndpoint: tokenURL})
	_, err := unserved.Rotate(context.Background(), self, publicJWK(t, next), "", "")
	assert.ErrorIs(t, err, app.ErrRotationUnavailable)

	svc, _ := newService(t)
	_, err = svc.Rotate(context.Background(), self, publicJWK(t, next), "", "")
	assert.ErrorIs(t, err, app.ErrInvalidProof)
}

// TestIsInstanceGRN_TakesTheFormInstanceGRNDerivesAndNoOther holds the
// GRN check the exchange, the rotation and the connection routes share.
func TestIsInstanceGRN_TakesTheFormInstanceGRNDerivesAndNoOther(t *testing.T) {
	derived, err := app.InstanceGRN(grn.Org("acme"), "5f60710a1b2c3d4e")
	require.NoError(t, err)
	assert.True(t, app.IsInstanceGRN(derived), "the control: InstanceGRN's own form")
	for _, raw := range []string{
		"grn:acme/iam/instance/5f60710a1b2c3d4e#somewhere",
		"grn:acme/iam/org/acme/instance/5f60710a1b2c3d4e",
		"grn:acme/iam/operator/one",
	} {
		g, err := grn.Parse(raw)
		require.NoError(t, err, raw)
		assert.False(t, app.IsInstanceGRN(g), raw)
	}
}
