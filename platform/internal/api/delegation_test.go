package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// doVersioned sends a request carrying the contract version header, or
// none where version is empty.
func (f *fixture) doVersioned(t *testing.T, method, path, version, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if version != "" {
		req.Header.Set(operator.ContractVersionHeader, version)
	}
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func grantBody(controller grn.GRN, expiresAt time.Time) string {
	return `{"requestId":"grant-1","controllers":["` + controller.String() +
		`"],"operations":["agent:create"],"expiresAt":"` + expiresAt.Format(time.RFC3339) + `"}`
}

// TestDelegation_RefusesAMissingOrUnsupportedVersion holds that every
// new route refuses a request naming no version or another one before
// anything is read, names the version it serves, and stamps it on the
// refusal. The supported version reaching the service is the control.
func TestDelegation_RefusesAMissingOrUnsupportedVersion(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "owner-sub", "Owner")
	path := "/orgs/acme/operators/hosted/delegation"

	for _, version := range []string{"", "operation-authority.v2"} {
		rec := f.doVersioned(t, "DELETE", path, version, "", cookie)
		require.Equal(t, http.StatusBadRequest, rec.Code, version)
		assert.Equal(t, operator.ContractVersion, rec.Header().Get(operator.ContractVersionHeader))
		assert.Contains(t, decodeBody[apigen.Error](t, rec).Message, operator.ContractVersion)
	}

	f.authority.EXPECT().EndGrant(mock.Anything, mock.Anything, mock.Anything, mock.Anything, operator.GrantID("grant-1")).
		Return(nil).Once()
	rec := f.doVersioned(t, "DELETE", path, operator.ContractVersion, `{"grantId":"grant-1"}`, cookie)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, operator.ContractVersion, rec.Header().Get(operator.ContractVersionHeader))
}

// TestWriteOperatorDelegation_PassesTheParsedWrite holds that a grant
// write reaches the service parsed, with the caller as actor and the
// expected grant it read, and that a created grant answers 201 and a
// replay 200.
func TestWriteOperatorDelegation_PassesTheParsedWrite(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "owner-sub", "Owner")
	orgG := orgGRNForTest("acme")
	hosted := operatorGRNOf("acme", "hosted")
	controller := operatorGRNOf("acme", "controller")
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	expected := operator.GrantID("grant-0")

	grant := &operator.Grant{ID: "grant-1", Org: orgG, Operator: hosted,
		Controllers: []grn.GRN{controller}, Operations: []operator.Operation{operator.OperationAgentCreate},
		ExpiresAt: expiresAt, GrantedBy: grn.New("root", "default", user.ServiceType, "owner-sub")}
	f.authority.EXPECT().WriteGrant(mock.Anything, mock.MatchedBy(func(w operator.GrantWrite) bool {
		return w.Org == orgG && w.Operator == hosted && w.RequestID == "grant-1" &&
			w.Expected != nil && *w.Expected == expected && w.Actor == grant.GrantedBy &&
			w.Terms.ExpiresAt.Equal(expiresAt) && len(w.Terms.Controllers) == 1
	})).Return(grant, true, nil).Once()

	body := strings.Replace(grantBody(controller, expiresAt), `{`, `{"expectedGrantId":"grant-0",`, 1)
	rec := f.doVersioned(t, "PUT", "/orgs/acme/operators/hosted/delegation", operator.ContractVersion, body, cookie)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeBody[apigen.Delegation](t, rec)
	assert.Equal(t, "grant-1", got.GrantId)
	assert.Equal(t, []string{controller.String()}, got.Controllers)

	f.authority.EXPECT().WriteGrant(mock.Anything, mock.Anything).Return(grant, false, nil).Once()
	rec = f.doVersioned(t, "PUT", "/orgs/acme/operators/hosted/delegation", operator.ContractVersion, body, cookie)
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestWriteOperatorDelegation_RefusesOverbroadTermsBeforeTheService
// holds that a controller of another organization, the hosted operator
// as its own controller, and an expiry past thirty days are refused at
// the boundary. The mock has no expectation, so a call fails the test.
func TestWriteOperatorDelegation_RefusesOverbroadTermsBeforeTheService(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "owner-sub", "Owner")
	soon := time.Now().Add(time.Hour)
	for name, body := range map[string]string{
		"another org's controller": grantBody(operatorGRNOf("globex", "controller"), soon),
		"itself as controller":     grantBody(operatorGRNOf("acme", "hosted"), soon),
		"past thirty days":         grantBody(operatorGRNOf("acme", "controller"), time.Now().Add(31*24*time.Hour)),
		"unknown operation":        strings.Replace(grantBody(operatorGRNOf("acme", "controller"), soon), "agent:create", "agent:delete", 1),
	} {
		rec := f.doVersioned(t, "PUT", "/orgs/acme/operators/hosted/delegation", operator.ContractVersion, body, cookie)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
}

// TestMintOperationAuthority_AnswersTheMintedAuthority holds that a
// mint reaches the service parsed and answers the authority once; a
// digest that is not lowercase hexadecimal never reaches it.
func TestMintOperationAuthority_AnswersTheMintedAuthority(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "member-sub", "Member")
	digest := strings.Repeat("ab", 32)
	body := `{"audience":"` + operatorGRNOf("acme", "hosted").String() + `","operation":"agent:create","target":"` +
		operatorGRNOf("acme", "controller").String() + `","requestId":"req-1","bodySha256":"` + digest + `"}`

	rec := f.doVersioned(t, "POST", "/orgs/acme/operation-authorities", operator.ContractVersion,
		strings.Replace(body, digest, strings.ToUpper(digest), 1), cookie)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	expiresAt := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)
	f.authority.EXPECT().Mint(mock.Anything, mock.MatchedBy(func(r operator.MintRequest) bool {
		return r.Org == orgGRNForTest("acme") && r.Operation == operator.OperationAgentCreate &&
			r.Digest == operator.BodyDigest(digest) && r.RequestID == "req-1" &&
			r.Actor == grn.New("root", "default", user.ServiceType, "member-sub")
	})).Return(&operator.Minted{Authority: "secret", ExpiresAt: expiresAt, Ref: "ref-1", Grant: "grant-1"}, nil).Once()
	rec = f.doVersioned(t, "POST", "/orgs/acme/operation-authorities", operator.ContractVersion, body, cookie)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeBody[apigen.OperationAuthority](t, rec)
	assert.Equal(t, apigen.OperationAuthority{Authority: "secret", ExpiresAt: expiresAt, OperationRef: "ref-1", GrantId: "grant-1"}, got)
}

// TestMintOperationAuthority_MapsRefusals holds the statuses the
// contract names for the service's refusals, and that a failure to
// decide answers 503 naming no cause.
func TestMintOperationAuthority_MapsRefusals(t *testing.T) {
	body := `{"audience":"` + operatorGRNOf("acme", "hosted").String() + `","operation":"agent:configure","target":"` +
		grn.New("acme", "default", "agent", "a1").String() + `","requestId":"req-1","bodySha256":"` + strings.Repeat("0", 64) + `"}`
	cases := []struct {
		err    error
		status int
	}{
		{operator.ErrNotDelegated, http.StatusForbidden},
		{operator.ErrAuthorityRequestConflict, http.StatusConflict},
		{errorx.Wrap(errorx.New(errorx.KindUnimplemented, "CheckCurrent"), "current authorization", errorx.KindUnavailable), http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		f := newFixture(t)
		cookie := f.sessionCookieFor(t, "member-sub", "Member")
		f.authority.EXPECT().Mint(mock.Anything, mock.Anything).Return(nil, c.err).Once()
		rec := f.doVersioned(t, "POST", "/orgs/acme/operation-authorities", operator.ContractVersion, body, cookie)
		assert.Equal(t, c.status, rec.Code, c.err.Error())
		if c.status == http.StatusServiceUnavailable {
			assert.Equal(t, "internal error", decodeBody[apigen.Error](t, rec).Message)
		}
	}
}

// TestGetOperatorDelegation_IsAMemberRead holds that reading the grant
// is authorized as other reads are, and that a caller denied
// operator:read never reaches the service.
func TestGetOperatorDelegation_IsAMemberRead(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "member-sub", "Member")
	slug := createOrg(t, f, cookie, "acme")
	path := "/orgs/" + slug + "/operators/hosted/delegation"

	f.permit()
	rec := f.doVersioned(t, "GET", path, operator.ContractVersion, "", cookie)
	require.Equal(t, http.StatusForbidden, rec.Code)

	f = newFixture(t)
	cookie = f.sessionCookieFor(t, "member-sub", "Member")
	slug = createOrg(t, f, cookie, "acme")
	path = "/orgs/" + slug + "/operators/hosted/delegation"
	f.permit(permission{grn.New("root", "default", user.ServiceType, "member-sub"), org.ActionOperatorRead})
	f.authority.EXPECT().CurrentGrant(mock.Anything, orgGRNForTest(slug), operatorGRNOf(slug, "hosted")).
		Return(&operator.Grant{ID: "grant-1", Operator: operatorGRNOf(slug, "hosted")}, nil).Once()
	rec = f.doVersioned(t, "GET", path, operator.ContractVersion, "", cookie)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "grant-1", decodeBody[apigen.Delegation](t, rec).GrantId)
}
