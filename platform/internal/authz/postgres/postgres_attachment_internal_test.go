// Internal test package, for the reason
// postgres_policy_internal_test.go states.
package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authz/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
)

// attachmentRow is a stored attachment row that parses.
func attachmentRow() db.PolicyAttachment {
	return db.PolicyAttachment{
		PolicyGrn:  "grn:acme:default:policy:9f2ac1b40d8e7a35",
		SubjectGrn: "grn:acme:default:group:eng",
		CreatedAt:  pgtype.Timestamptz{Valid: true},
	}
}

// activePolicyRow is a stored active-policy row that parses.
func activePolicyRow() db.ActivePoliciesRow {
	return db.ActivePoliciesRow{
		Grn:         "grn:acme:default:policy:9f2ac1b40d8e7a35",
		Document:    policyVersionRow().Document,
		SubjectGrns: []string{"grn:acme:default:group:eng"},
	}
}

// TestRowToAttachment_CorruptFieldIsInternal is
// TestRowToPolicy_CorruptFieldIsInternal for the attachment row.
func TestRowToAttachment_CorruptFieldIsInternal(t *testing.T) {
	ok, err := rowToAttachment(attachmentRow())
	require.NoError(t, err, "the row that parses is the control")
	require.Equal(t, "eng", ok.Subject.Identifier)

	corruptions := map[string]func(*db.PolicyAttachment){
		"policy grn":  func(row *db.PolicyAttachment) { row.PolicyGrn = corrupt },
		"subject grn": func(row *db.PolicyAttachment) { row.SubjectGrn = corrupt },
	}
	for name, corruptField := range corruptions {
		t.Run(name, func(t *testing.T) {
			row := attachmentRow()
			corruptField(&row)
			_, err := rowToAttachment(row)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v: %v", errorx.KindOf(err), err)
		})
	}
}

// TestRowToActivePolicy_CorruptFieldIsInternal is
// TestRowToPolicy_CorruptFieldIsInternal for the row a check is
// evaluated from.
func TestRowToActivePolicy_CorruptFieldIsInternal(t *testing.T) {
	ok, err := rowToActivePolicy(activePolicyRow())
	require.NoError(t, err, "the row that parses is the control")
	require.Len(t, ok.Subjects, 1)
	require.Equal(t, "9f2ac1b40d8e7a35", ok.GRN.Identifier)

	corruptions := map[string]func(*db.ActivePoliciesRow){
		"policy grn":  func(row *db.ActivePoliciesRow) { row.Grn = corrupt },
		"document":    func(row *db.ActivePoliciesRow) { row.Document = []byte(`{"Statement":[]}`) },
		"subject grn": func(row *db.ActivePoliciesRow) { row.SubjectGrns = []string{corrupt} },
	}
	for name, corruptField := range corruptions {
		t.Run(name, func(t *testing.T) {
			row := activePolicyRow()
			corruptField(&row)
			_, err := rowToActivePolicy(row)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v: %v", errorx.KindOf(err), err)
		})
	}
}
