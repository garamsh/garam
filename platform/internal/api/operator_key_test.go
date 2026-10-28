package api_test

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
)

// keyFixture is an organization with one registered operator, an admin
// holding operator:register and operator:reissue, a registrar holding
// operator:register alone and a reader holding operator:read.
type keyFixture struct {
	*fixture
	slug                     string
	admin, registrar, reader *http.Cookie
}

func newKeyFixture(t *testing.T, cfg api.Config) keyFixture {
	t.Helper()
	f := newFixtureConfigured(t, cfg)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("admin-sub"), org.ActionOperatorReissue},
		permission{userGRNOf("admin-sub"), org.ActionOperatorRead},
		permission{userGRNOf("registrar-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("reader-sub"), org.ActionOperatorRead},
	)
	for sub, role := range map[string]org.Role{"admin-sub": org.RoleAdmin, "registrar-sub": org.RoleMember, "reader-sub": org.RoleMember} {
		require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf(sub), role)))
	}
	k := keyFixture{fixture: f, slug: slug,
		admin:     f.sessionCookieFor(t, "admin-sub", "Admin"),
		registrar: f.sessionCookieFor(t, "registrar-sub", "Registrar"),
		reader:    f.sessionCookieFor(t, "reader-sub", "Reader"),
	}
	for _, id := range []string{"controller", "control"} {
		rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"`+id+`"}`, k.admin)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	return k
}

func (k keyFixture) issue(t *testing.T, id, rotation string, as *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return k.do(t, "POST", "/orgs/"+k.slug+"/operators/"+id+"/keys", `{"rotation":"`+rotation+`"}`, as)
}

// TestIssueOperatorKey_AnswersThePEMOnceWithItsIdentityAndTokenURL is
// the issuance seen from the wire (ADR-0102): the PEM is a PKCS#8 RSA
// key of 3072 bits whose thumbprint is the fingerprint answered beside
// it, the answer names the operator's GRN and the configured token
// URL and is not cached, and what the store kept and the list answers
// is the public record alone.
func TestIssueOperatorKey_AnswersThePEMOnceWithItsIdentityAndTokenURL(t *testing.T) {
	k := newKeyFixture(t, api.Config{SessionTTL: time.Hour, TokenURL: fixtureTokenURL})

	rec := k.issue(t, "controller", "planned", k.admin)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	issued := decodeBody[apigen.IssuedOperatorKey](t, rec)
	assert.Equal(t, operatorGRNOf(k.slug, "controller").String(), issued.ClientId)
	assert.Equal(t, fixtureTokenURL, issued.TokenUrl, "the token URL is the configured value, byte for byte")

	block, _ := pem.Decode([]byte(issued.PrivateKeyPem))
	require.NotNil(t, block)
	assert.Equal(t, "PRIVATE KEY", block.Type)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	private, ok := parsed.(*rsa.PrivateKey)
	require.True(t, ok)
	assert.Equal(t, operator.IssuedKeyBits, private.N.BitLen())
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, kid, issued.Key.Kid, "the fingerprint answered is the PEM's own")
	assert.Equal(t, apigen.OperatorKeyActive, issued.Key.State)

	held, err := k.operators.ListKeys(testCtx(t), operatorGRNOf(k.slug, "controller"))
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, private.PublicKey.N, held[0].Public.N, "the store holds the public half")

	rec = k.do(t, "GET", "/orgs/"+k.slug+"/operators/controller/keys", "", k.reader)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "PRIVATE KEY", "the list answers no private key")
	listed := decodeBody[apigen.OperatorKeyList](t, rec)
	require.Len(t, listed.Keys, 1)
	assert.Equal(t, kid, listed.Keys[0].Kid)
}

// TestIssueOperatorKey_TheRegistrationsAuthorityIssuesTheFirstKeyAlone
// holds the PM's decision for #1356: operator:register issues a first
// key to an operator holding no live one, whatever rotation it asks
// for, and is refused once a live key exists; operator:reissue is the
// control, issuing the planned successor the registrar could not.
func TestIssueOperatorKey_TheRegistrationsAuthorityIssuesTheFirstKeyAlone(t *testing.T) {
	k := newKeyFixture(t, api.Config{SessionTTL: time.Hour, TokenURL: fixtureTokenURL})

	rec := k.issue(t, "controller", "immediate", k.registrar)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []operator.KeyRotation{operator.KeyRotationFirst}, k.operators.issuances,
		"a registrar issues under first, not the rotation it asked")

	rec = k.issue(t, "controller", "planned", k.registrar)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a live key puts the next issuance under operator:reissue")

	rec = k.issue(t, "controller", "planned", k.admin)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, operator.KeyRotationPlanned, k.operators.issuances[len(k.operators.issuances)-1])

	rec = k.issue(t, "control", "planned", k.reader)
	assert.Equal(t, http.StatusForbidden, rec.Code, "operator:read issues nothing")
}

// TestIssueOperatorKey_IssuesNothingWithoutATokenURL is the PM's
// decision on GARAM_MACHINE_TOKEN_URL (#1394): unset, the route answers
// 409 failed_precondition with the key_issuance_unconfigured reason,
// and no key is generated or stored. The same fixture with the URL set
// is the control: it issues.
func TestIssueOperatorKey_IssuesNothingWithoutATokenURL(t *testing.T) {
	k := newKeyFixture(t, api.Config{SessionTTL: time.Hour})

	rec := k.issue(t, "controller", "planned", k.admin)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	body := decodeBody[apigen.Error](t, rec)
	assert.Equal(t, "failed_precondition", body.Kind)
	require.NotNil(t, body.Reason, "the refusal names its reason")
	assert.Equal(t, operator.ReasonKeyIssuanceUnconfigured, *body.Reason)
	assert.Nil(t, body.Path, "the reason is about no document")
	assert.NotContains(t, rec.Body.String(), "PRIVATE KEY")
	assert.Empty(t, k.operators.issuances, "nothing reached the store")

	configured := newKeyFixture(t, api.Config{SessionTTL: time.Hour, TokenURL: fixtureTokenURL})
	rec = configured.issue(t, "controller", "planned", configured.admin)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// TestIssueOperatorKey_IsBoundedPerOperator holds the per-process
// bound on issuance (ADR-0102): the fourth key one operator is issued
// inside the window is refused with 429 and Retry-After, and another
// operator in the same window is the control.
func TestIssueOperatorKey_IsBoundedPerOperator(t *testing.T) {
	k := newKeyFixture(t, api.Config{SessionTTL: time.Hour, TokenURL: fixtureTokenURL})

	for range operator.KeyIssuancesPerOperator {
		rec := k.issue(t, "controller", "immediate", k.admin)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	rec := k.issue(t, "controller", "immediate", k.admin)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.Len(t, k.operators.issuances, operator.KeyIssuancesPerOperator, "the refused issuance stored nothing")

	rec = k.issue(t, "control", "immediate", k.admin)
	assert.Equal(t, http.StatusCreated, rec.Code, "another operator has its own bound")
}

// TestRevokeOperatorKey_WithdrawsUnderTheReissueAuthority holds the
// withdrawal (ADR-0102): operator:reissue withdraws a key, which lists
// as revoked; the registrar is refused; an unknown fingerprint is 404.
func TestRevokeOperatorKey_WithdrawsUnderTheReissueAuthority(t *testing.T) {
	k := newKeyFixture(t, api.Config{SessionTTL: time.Hour, TokenURL: fixtureTokenURL})
	rec := k.issue(t, "controller", "planned", k.admin)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	kid := decodeBody[apigen.IssuedOperatorKey](t, rec).Key.Kid
	path := "/orgs/" + k.slug + "/operators/controller/keys/"

	assert.Equal(t, http.StatusForbidden, k.do(t, "DELETE", path+kid, "", k.registrar).Code)
	assert.Equal(t, http.StatusNoContent, k.do(t, "DELETE", path+kid, "", k.admin).Code)
	assert.Equal(t, http.StatusNotFound, k.do(t, "DELETE", path+"bm90LWEta2V5", "", k.admin).Code)

	listed := decodeBody[apigen.OperatorKeyList](t, k.do(t, "GET", "/orgs/"+k.slug+"/operators/controller/keys", "", k.reader))
	require.Len(t, listed.Keys, 1)
	assert.Equal(t, apigen.OperatorKeyRevoked, listed.Keys[0].State)
}

// TestIssueOperatorKey_RegisterCannotReKeyAfterARevocation is the PM's
// review of #1360, blocking item 1: once an operator has been issued a
// key, withdrawing it leaves no first issuance for operator:register to
// make, so the registrar is refused where operator:reissue, the
// control, issues the replacement.
func TestIssueOperatorKey_RegisterCannotReKeyAfterARevocation(t *testing.T) {
	k := newKeyFixture(t, api.Config{SessionTTL: time.Hour, TokenURL: fixtureTokenURL})
	rec := k.issue(t, "controller", "planned", k.registrar)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	kid := decodeBody[apigen.IssuedOperatorKey](t, rec).Key.Kid
	require.Equal(t, http.StatusNoContent,
		k.do(t, "DELETE", "/orgs/"+k.slug+"/operators/controller/keys/"+kid, "", k.admin).Code)

	rec = k.issue(t, "controller", "immediate", k.registrar)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a revoked key still makes the next issuance operator:reissue")
	assert.NotContains(t, rec.Body.String(), "PRIVATE KEY")

	rec = k.issue(t, "controller", "immediate", k.admin)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: operator:reissue re-keys: %s", rec.Body.String())
}
