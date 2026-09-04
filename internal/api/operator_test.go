package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// TestOperatorRegister_AdminRegistersWhereOthersAreRefused holds the
// authorization shape every org resource takes: the policy engine
// decides an action of its own, and org membership is the precondition.
//
// The outsider is permitted the action and belongs to another org; the
// stranger belongs to this one and is permitted nothing. Each isolates
// one half of the check, and the admin is the case that stops being
// answered when either half is removed.
func TestOperatorRegister_AdminRegistersWhereOthersAreRefused(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")

	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("admin-sub"), org.ActionOperatorRead},
		permission{userGRNOf("outsider-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("stranger-sub"), org.ActionOperatorRead},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	registered := decodeBody[apigen.RegisteredOperator](t, rec)
	assert.Equal(t, operatorGRNOf(slug, "field-one").String(), registered.Operator.Grn)
	assert.Equal(t, orgGRNOf(slug).String(), registered.Operator.OrgGrn)
	assert.Equal(t, userGRNOf("admin-sub").String(), registered.Operator.RegisteredBy)

	// A member of another org is refused the action it is permitted,
	// and their own org answers the same request.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two"}`, outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+otherSlug+"/operators", `{"identifier":"field-two"}`, outsider)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// A member of this org the evaluator denies is refused, so
	// membership alone does not admit. Reading is permitted to that
	// member, so what it lacks is the write.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two"}`, stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", stranger)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Signed out is refused, and the same request carrying a session is
	// answered.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two"}`, admin)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// The org's operators are its own.
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", admin)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.Operator](t, rec)
	require.Len(t, listed, 2, "this org's operators and no other org's")
	assert.Equal(t, operatorGRNOf(slug, "field-one").String(), listed[0].Grn)
	assert.Equal(t, operatorGRNOf(slug, "field-two").String(), listed[1].Grn)
}

// TestOperatorRegister_RefusesWhatItCannotStore covers the boundary the
// identifier crosses. It is supplied by the caller rather than minted
// (ADR-0045), so it is the one org resource where a person's typing
// reaches a GRN segment directly.
//
// The accepted registration at the end is the control: every refusal
// above stops holding for it.
func TestOperatorRegister_RefusesWhatItCannotStore(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("admin-sub"), org.ActionOperatorRegister})
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))

	tests := []struct {
		name string
		body string
	}{
		{"no identifier", `{}`},
		{"empty identifier", `{"identifier":""}`},
		{"a colon, which re-associates the segments", `{"identifier":"field:one"}`},
		{"uppercase", `{"identifier":"Field-One"}`},
		{"body that is not an object", `[]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := f.do(t, "POST", "/orgs/"+slug+"/operators", tt.body, admin)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)

	// Registering the same identifier twice is a conflict rather than a
	// second record: the caller learns the identifier is taken.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// TestOperatorRegister_AnswersATokenThatEnrolsThatOperatorAndNothingElse
// covers what the registration hands back (ADR-0064): a token that
// enrols the operator just registered, minted afresh each time, and
// held nowhere a route can read it out again.
func TestOperatorRegister_AnswersATokenThatEnrolsThatOperatorAndNothingElse(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("admin-sub"), org.ActionOperatorRead},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	first := decodeBody[apigen.RegisteredOperator](t, rec)
	require.NotEmpty(t, first.Enrollment.Token)

	// The token enrols the operator it was minted for and no other.
	enrolled, ok := f.operators.enrolls(first.Enrollment.Token)
	require.True(t, ok)
	assert.Equal(t, operatorGRNOf(slug, "field-one"), enrolled)
	assert.Equal(t, operator.EnrollmentTTL,
		first.Enrollment.ExpiresAt.Sub(first.Operator.CreatedAt).Round(time.Second))

	// A second registration mints a second token, so no two operators
	// share one.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	second := decodeBody[apigen.RegisteredOperator](t, rec)
	assert.NotEqual(t, first.Enrollment.Token, second.Enrollment.Token)

	// The list answers the records and carries no token: this is the
	// only response the token was ever in.
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", admin)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), first.Enrollment.Token)
	assert.NotContains(t, rec.Body.String(), "token")
}

// TestOperatorList_CarriesTheEnrollmentStateAndNoCertificateDetail
// covers what the listing now says about enrolment. A registration
// answers `pending` because it minted a token nobody has spent, which
// is the one state this fixture reaches: what tells the three apart is
// the query, and that is covered against real Postgres in
// tests/integration.
//
// certificateIssuedAt is absent rather than zero. The two are different
// states and only the absence is the property — a zero date renders as
// a date. The response carries no serial either: which certificate an
// operator holds decides a renewal and belongs to the machine listener
// (ADR-0046).
func TestOperatorList_CarriesTheEnrollmentStateAndNoCertificateDetail(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("admin-sub"), org.ActionOperatorRead},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	registered := decodeBody[apigen.RegisteredOperator](t, rec)
	assert.Equal(t, apigen.OperatorEnrollmentStatePending, registered.Operator.EnrollmentState)
	assert.Nil(t, registered.Operator.CertificateIssuedAt)

	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", admin)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.Operator](t, rec)
	require.Len(t, listed, 1)
	assert.Equal(t, apigen.OperatorEnrollmentStatePending, listed[0].EnrollmentState)
	assert.Nil(t, listed[0].CertificateIssuedAt)
	assert.NotContains(t, rec.Body.String(), "certificateIssuedAt",
		"an absent timestamp is omitted rather than answered as a zero date")
	assert.NotContains(t, rec.Body.String(), "serial")

	// The fields an entry answers with, so one added without a decision
	// is visible here. `registeredBy` and `createdAt` are what the
	// state was added beside, not what it replaces.
	entries := decodeBody[[]map[string]json.RawMessage](t, rec)
	require.Len(t, entries, 1)
	fields := make([]string, 0, len(entries[0]))
	for k := range entries[0] {
		fields = append(fields, k)
	}
	slices.Sort(fields)
	assert.Equal(t, []string{"createdAt", "enrollmentState", "grn", "orgGrn", "registeredBy"}, fields)
}
