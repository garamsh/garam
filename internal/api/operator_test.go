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
	assert.Equal(t, []string{"createdAt", "description", "enrollmentState", "grn", "name", "orgGrn", "registeredBy"}, fields)
}

// TestOperatorMintEnrollment_AdminMintsWhereOthersAreRefused holds the
// re-mint to the authorization registration already has (ADR-0067). The
// two are the same action on the same object, so the shape here is the
// registration test's and the cases are chosen for the same reason: the
// outsider holds the action in another org, the stranger holds
// membership and not the action, and the admin is what stops being
// answered if either half of the check goes.
func TestOperatorMintEnrollment_AdminMintsWhereOthersAreRefused(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")

	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("outsider-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("stranger-sub"), org.ActionOperatorRead},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin).Code)
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/orgs/"+otherSlug+"/operators", `{"identifier":"field-one"}`, outsider).Code)

	path := "/orgs/" + slug + "/operators/field-one/enrollment"

	// A member of another org is refused the action it is permitted,
	// and the same request in its own org is answered.
	rec := f.do(t, "POST", path, "", outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+otherSlug+"/operators/field-one/enrollment", "", outsider)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// A member of this org the evaluator denies the write is refused,
	// so membership alone does not admit. It may read the operators.
	rec = f.do(t, "POST", path, "", stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", stranger)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Signed out is refused, and the same request carrying a session is
	// answered — the control for every refusal above.
	rec = f.do(t, "POST", path, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = f.do(t, "POST", path, "", admin)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

// TestOperatorMintEnrollment_ReplacesTheTokenAndRefusesAnEnrolledOne
// covers what the route answers and what it refuses (ADR-0067).
//
// The two refusals carry the accepted mint as their control: an
// operator with no certificate is minted for on the same call that the
// unregistered one and the enrolled one are refused on, so what refuses
// them is the case each names rather than the route refusing
// everything.
//
// That a replaced token stops enrolling is asserted through the store
// rather than through a second HTTP call, because no route spends one:
// spending is the machine listener's, and this fixture serves the
// browser-facing one.
func TestOperatorMintEnrollment_ReplacesTheTokenAndRefusesAnEnrolledOne(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("admin-sub"), org.ActionOperatorRegister})
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	registered := decodeBody[apigen.RegisteredOperator](t, rec)

	// The registration's token enrols the operator, which is what has
	// to stop being true below.
	enrolls, ok := f.operators.enrolls(registered.Enrollment.Token)
	require.True(t, ok)
	require.Equal(t, operatorGRNOf(slug, "field-one"), enrolls)

	rec = f.do(t, "POST", "/orgs/"+slug+"/operators/field-one/enrollment", "", admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	minted := decodeBody[apigen.OperatorEnrollment](t, rec)
	require.NotEmpty(t, minted.Token)
	assert.NotEqual(t, registered.Enrollment.Token, minted.Token)
	assert.Equal(t, operator.EnrollmentTTL,
		minted.ExpiresAt.Sub(registered.Operator.CreatedAt).Round(time.Second))

	// The new token enrols the operator and the old one no longer
	// enrols anything: two live tokens would be two credential paths
	// for one identity.
	enrolls, ok = f.operators.enrolls(minted.Token)
	require.True(t, ok)
	assert.Equal(t, operatorGRNOf(slug, "field-one"), enrolls)
	_, ok = f.operators.enrolls(registered.Enrollment.Token)
	assert.False(t, ok, "the token the re-mint replaced enrols nothing")

	// An operator the org never registered is a miss, not a mint.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators/never-registered/enrollment", "", admin)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// An identifier no GRN segment can hold is refused before any
	// record is looked for.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators/Field-One/enrollment", "", admin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// An operator that already enrolled is refused, and the refusal
	// says a decision was taken rather than reporting a failure.
	f.operators.markEnrolled(operatorGRNOf(slug, "field-one"))
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators/field-one/enrollment", "", admin)
	require.Equal(t, http.StatusConflict, rec.Code)
	refused := decodeBody[apigen.Error](t, rec)
	assert.Equal(t, "failed_precondition", refused.Kind)
	assert.Contains(t, refused.Message, "already enrolled")
	assert.Contains(t, refused.Message, "second credential path")
	assert.NotContains(t, rec.Body.String(), minted.Token,
		"a refusal answers no token")

	// The control: the operator beside it holds no certificate and is
	// minted for on the same call, so what refused the one above is the
	// certificate and not the route.
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two"}`, admin).Code)
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators/field-two/enrollment", "", admin)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

// TestOperatorDelete_AdminDeletesWhereOthersAreRefused holds the
// authorization the delete takes, which is not the one that registers:
// ADR-0070 gives it an action of its own, so a member permitted to
// register is not thereby permitted to stop an operator.
//
// The registrar is that control. It is permitted `operator:register`
// and refused here, and its own registration is answered, so what
// refuses its delete is the action rather than the membership or the
// session. The outsider and the signed-out caller isolate the other
// two halves the same way.
func TestOperatorDelete_AdminDeletesWhereOthersAreRefused(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("admin-sub"), org.ActionOperatorDelete},
		permission{userGRNOf("owner-sub"), org.ActionOperatorRead},
		permission{userGRNOf("registrar-sub"), org.ActionOperatorRegister},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	registrar := f.sessionCookieFor(t, "registrar-sub", "Registrar")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("registrar-sub"), org.RoleMember)))

	register := func(identifier string, cookie *http.Cookie) {
		t.Helper()
		rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"`+identifier+`"}`, cookie)
		require.Equal(t, http.StatusCreated, rec.Code)
	}
	register("field-one", admin)

	// A member holding the action that registers may register and may
	// not delete.
	register("field-two", registrar)
	rec := f.do(t, "DELETE", "/orgs/"+slug+"/operators/field-one", "", registrar)
	assert.Equal(t, http.StatusForbidden, rec.Code,
		"registering an operator does not carry deleting one")

	// Signed out is refused, and the same request carrying the admin's
	// session is answered.
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/field-one", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/field-one", "", admin)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// The record is gone from the list, and the identifier is free
	// again: registering it a second time is answered rather than
	// refused as already registered.
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.Operator](t, rec)
	for _, o := range listed {
		assert.NotEqual(t, operatorGRNOf(slug, "field-one").String(), o.Grn)
	}
	register("field-one", admin)
}

// TestOperatorDelete_AnswersTheStateTheOperatorIsIn covers the three
// answers the route gives beside the deletion itself. Each sits beside
// a delete that succeeds, because a route refusing every call would
// answer all three the same way.
func TestOperatorDelete_AnswersTheStateTheOperatorIsIn(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("owner-sub"), org.ActionOperatorDelete},
		permission{userGRNOf("owner-sub"), org.ActionOperatorRead},
	)

	// An operator that never enrolled is deleted by the same call and
	// is not a special case: nothing here asks whether a certificate
	// was ever issued.
	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"never-enrolled"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/never-enrolled", "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// An operator that enrolled is deleted by that same call.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"enrolled"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	f.operators.markEnrolled(operatorGRNOf(slug, "enrolled"))
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/enrolled", "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// An operator the org holds no record of is a missing resource.
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/never-registered", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// An operator a definition names is refused, with the sentence that
	// says what to do rather than a bare conflict, and nothing is
	// deleted: the same operator is still there to delete once the
	// definition is not naming it.
	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"named"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	f.operators.markNamedByDefinition(operatorGRNOf(slug, "named"))
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/named", "", owner)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"kind":"failed_precondition"`)
	assert.Contains(t, rec.Body.String(), "agent definition names this operator")
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), operatorGRNOf(slug, "named").String(),
		"a refused delete removes nothing")
}

// TestOperatorRegister_NamesTheOperatorOrCallsItByItsIdentifier covers
// the details a registration starts with (ADR-0074). A caller that
// sends only the identifier registers as before and is answered a name
// equal to it; one that names the operator is answered what it sent,
// trimmed. A name the domain refuses refuses the registration, and the
// identifier it carried is still free afterwards: nothing was written.
func TestOperatorRegister_NamesTheOperatorOrCallsItByItsIdentifier(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionOperatorRegister})

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	unnamed := decodeBody[apigen.RegisteredOperator](t, rec)
	assert.Equal(t, "field-one", unnamed.Operator.Name)
	assert.Empty(t, unnamed.Operator.Description)

	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two","name":"   "}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/operators",
		`{"identifier":"field-two","name":" Field cluster two ","description":"Batch agents."}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	named := decodeBody[apigen.RegisteredOperator](t, rec)
	assert.Equal(t, "Field cluster two", named.Operator.Name)
	assert.Equal(t, "Batch agents.", named.Operator.Description)
}

// TestOperatorGet_AnswersOneOperatorOfThisOrg covers the read a
// detail page is built on. The admin's read of its own org is the
// control for the two refusals: the outsider is permitted the read and
// belongs to another org, and the operator that was never registered
// is a missing resource rather than a refusal.
func TestOperatorGet_AnswersOneOperatorOfThisOrg(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("owner-sub"), org.ActionOperatorRead},
		permission{userGRNOf("outsider-sub"), org.ActionOperatorRead},
	)
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one","name":"Field one"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = f.do(t, "GET", "/orgs/"+slug+"/operators/field-one", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[apigen.Operator](t, rec)
	assert.Equal(t, operatorGRNOf(slug, "field-one").String(), got.Grn)
	assert.Equal(t, "Field one", got.Name)
	assert.Equal(t, apigen.OperatorEnrollmentStatePending, got.EnrollmentState)

	rec = f.do(t, "GET", "/orgs/"+slug+"/operators/field-one", "", outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators/never-registered", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/operators/field-one", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestOperatorUpdate_AdminUpdatesWhereOthersAreRefused holds the update
// to an action of its own (ADR-0074). The registrar is permitted both
// actions that register and delete and is refused here, so what
// refuses it is the missing `operator:update` and not the membership;
// the outsider holds that action in another org, so what refuses it is
// the org. The admin's update is the control for both, and the
// signed-out call beside it isolates the session.
func TestOperatorUpdate_AdminUpdatesWhereOthersAreRefused(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("admin-sub"), org.ActionOperatorUpdate},
		permission{userGRNOf("registrar-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("registrar-sub"), org.ActionOperatorDelete},
		permission{userGRNOf("outsider-sub"), org.ActionOperatorUpdate},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	registrar := f.sessionCookieFor(t, "registrar-sub", "Registrar")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("registrar-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-one"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)

	path := "/orgs/" + slug + "/operators/field-one"
	rec = f.do(t, "PATCH", path, `{"name":"Field one"}`, registrar)
	assert.Equal(t, http.StatusForbidden, rec.Code,
		"registering and deleting an operator do not carry renaming one")
	rec = f.do(t, "PATCH", path, `{"name":"Field one"}`, outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "PATCH", path, `{"name":"Field one"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = f.do(t, "PATCH", path, `{"name":"Field one"}`, admin)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Field one", decodeBody[apigen.Operator](t, rec).Name)
}

// TestOperatorUpdate_ChangesTheFieldsItNamesAndNothingElse covers what
// a patch moves. A field left out keeps its value, an empty
// description clears the one there was, and the identifier the path
// carries is the GRN before and after: a name is not an identity. A
// refused patch changes nothing, which the read after it shows, and an
// operator the org never registered is a missing resource.
func TestOperatorUpdate_ChangesTheFieldsItNamesAndNothingElse(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("owner-sub"), org.ActionOperatorRead},
		permission{userGRNOf("owner-sub"), org.ActionOperatorUpdate},
	)
	rec := f.do(t, "POST", "/orgs/"+slug+"/operators",
		`{"identifier":"field-one","name":"Field one","description":"Batch agents."}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	path := "/orgs/" + slug + "/operators/field-one"

	rec = f.do(t, "PATCH", path, `{"name":"Renamed"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	renamed := decodeBody[apigen.Operator](t, rec)
	assert.Equal(t, "Renamed", renamed.Name)
	assert.Equal(t, "Batch agents.", renamed.Description, "a field left out keeps its value")
	assert.Equal(t, operatorGRNOf(slug, "field-one").String(), renamed.Grn)

	rec = f.do(t, "PATCH", path, `{"description":""}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	cleared := decodeBody[apigen.Operator](t, rec)
	assert.Equal(t, "Renamed", cleared.Name)
	assert.Empty(t, cleared.Description)

	rec = f.do(t, "PATCH", path, `{"name":""}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = f.do(t, "PATCH", path, `{"description":"a\u0000b"}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"NUL is the caller's error and not a storage one")
	rec = f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Renamed", decodeBody[apigen.Operator](t, rec).Name, "a refused patch changes nothing")

	rec = f.do(t, "PATCH", "/orgs/"+slug+"/operators/never-registered", `{"name":"Ghost"}`, owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
