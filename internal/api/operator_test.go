package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
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
	registered := decodeBody[apigen.Operator](t, rec)
	assert.Equal(t, operatorGRNOf(slug, "field-one").String(), registered.Grn)
	assert.Equal(t, orgGRNOf(slug).String(), registered.OrgGrn)
	assert.Equal(t, userGRNOf("admin-sub").String(), registered.RegisteredBy)

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

// TestOperatorRegister_AnswersTheRecordAloneAndListsIt covers what a
// registration hands back (ADR-0117): the operator it stored, with no
// enrollment token and no credential, since it issues neither; an
// operator enters by a key. The list answers the same record.
//
// certificateIssuedAt is absent rather than zero. The two are different
// states and only the absence is the property — a zero date renders as
// a date. The response carries no serial either: which certificate an
// operator holds decides a renewal and belongs to the machine listener
// (ADR-0046).
func TestOperatorRegister_AnswersTheRecordAloneAndListsIt(t *testing.T) {
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
	answered := fieldsOf(t, decodeBody[map[string]json.RawMessage](t, rec))
	assert.Equal(t, []string{"createdAt", "description", "grn", "name", "orgGrn", "registeredBy"}, answered,
		"the registration answers the record and nothing it would have issued")

	rec = f.do(t, "GET", "/orgs/"+slug+"/operators", "", admin)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.Operator](t, rec)
	require.Len(t, listed, 1)
	assert.Equal(t, operatorGRNOf(slug, "field-one").String(), listed[0].Grn)
	assert.Nil(t, listed[0].CertificateIssuedAt)
	assert.NotContains(t, rec.Body.String(), "certificateIssuedAt",
		"an absent timestamp is omitted rather than answered as a zero date")
	assert.NotContains(t, rec.Body.String(), "serial")

	// The fields an entry answers with, so one added without a decision
	// is visible here.
	entries := decodeBody[[]map[string]json.RawMessage](t, rec)
	require.Len(t, entries, 1)
	assert.Equal(t, answered, fieldsOf(t, entries[0]), "a listed entry is the record registration answered")
}

// fieldsOf answers the names of an object's fields, sorted.
func fieldsOf(t *testing.T, object map[string]json.RawMessage) []string {
	t.Helper()
	fields := make([]string, 0, len(object))
	for k := range object {
		fields = append(fields, k)
	}
	slices.Sort(fields)
	return fields
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

// TestOperatorDelete_AnswersTheStateTheOperatorIsIn covers the two
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

	// An operator is deleted whether or not it was ever issued a
	// certificate: nothing here asks.
	rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"never-enrolled"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/never-enrolled", "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// An operator the org holds no record of is a missing resource.
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/operators/never-registered", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
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
	unnamed := decodeBody[apigen.Operator](t, rec)
	assert.Equal(t, "field-one", unnamed.Name)
	assert.Empty(t, unnamed.Description)

	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"field-two","name":"   "}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/operators",
		`{"identifier":"field-two","name":" Field cluster two ","description":"Batch agents."}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	named := decodeBody[apigen.Operator](t, rec)
	assert.Equal(t, "Field cluster two", named.Name)
	assert.Equal(t, "Batch agents.", named.Description)
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
