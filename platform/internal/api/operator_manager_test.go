package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
)

// TestSetOperatorManager_LinksOnceUnderTheRegisteringAction covers the
// set-once link (ADR-0124): a registrar links a control service to its
// cluster manager once, and a second set is refused whichever manager
// it names. A member permitted only to read is refused, and a value
// that is no record of this organization is refused before the store.
// The first set is the control for every refusal.
func TestSetOperatorManager_LinksOnceUnderTheRegisteringAction(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionOperatorRegister},
		permission{userGRNOf("reader-sub"), org.ActionOperatorRead},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	reader := f.sessionCookieFor(t, "reader-sub", "Reader")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("reader-sub"), org.RoleMember)))
	for _, id := range []string{"aurora", "aurora-control", "borealis"} {
		rec := f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"`+id+`"}`, admin)
		require.Equal(t, http.StatusCreated, rec.Code)
	}
	link := func(record, manager string, cookie *http.Cookie) int {
		t.Helper()
		return f.do(t, "PUT", "/orgs/"+slug+"/operators/"+record+"/manager",
			`{"manager":"`+manager+`"}`, cookie).Code
	}
	aurora := operatorGRNOf(slug, "aurora").String()

	assert.Equal(t, http.StatusForbidden, link("aurora-control", aurora, reader), "read alone links nothing")
	assert.Equal(t, http.StatusBadRequest, link("aurora-control", "aurora", admin), "not a grn")
	assert.Equal(t, http.StatusBadRequest, link("aurora-control", operatorGRNOf(slug, "aurora-control").String(), admin), "itself")
	assert.Equal(t, http.StatusConflict, link("aurora-control", operatorGRNOf(slug, "nobody").String(), admin), "not registered")
	assert.Equal(t, http.StatusNotFound, link("nobody", aurora, admin), "no such record")

	rec := f.do(t, "PUT", "/orgs/"+slug+"/operators/aurora-control/manager", `{"manager":"`+aurora+`"}`, admin)
	require.Equal(t, http.StatusOK, rec.Code)
	linked := decodeBody[apigen.Operator](t, rec)
	require.NotNil(t, linked.Manager)
	assert.Equal(t, aurora, *linked.Manager)

	assert.Equal(t, http.StatusConflict, link("aurora-control", aurora, admin), "the same manager again")
	assert.Equal(t, http.StatusConflict, link("aurora-control", operatorGRNOf(slug, "borealis").String(), admin), "another manager")
	assert.Equal(t, http.StatusConflict, link("borealis", operatorGRNOf(slug, "aurora-control").String(), admin), "a manager that is a control service")
}
