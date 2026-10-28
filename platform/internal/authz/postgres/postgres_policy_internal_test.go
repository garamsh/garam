// Internal test package: the row parsers are unexported and the only
// exported path to them holds a database, so the kind a corrupt row
// answers with is reachable from here alone.
package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/authz/postgres/db"
	"github.com/garamsh/garam/platform/internal/wire"
)

const corrupt = "not a grn"

// policyRow is a stored policy row that parses, for a case to corrupt
// one field of.
func policyRow() db.Policy {
	return db.Policy{
		Grn:            "grn:acme:default:policy:9f2ac1b40d8e7a35",
		OrgGrn:         "grn:root:default:org:acme",
		Name:           "read-only",
		Status:         string(wire.PolicyStatusDraft),
		DefaultVersion: 1,
	}
}

// policyVersionRow is a stored version row that parses.
func policyVersionRow() db.PolicyVersion {
	return db.PolicyVersion{
		PolicyGrn: "grn:acme:default:policy:9f2ac1b40d8e7a35",
		Version:   1,
		Document:  []byte(`{"Version":"2026-07-25","Statement":[{"Sid":"s","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`),
		CreatedBy: "grn:root:default:user:alice",
		CreatedAt: pgtype.Timestamptz{Valid: true},
	}
}

// TestRowToPolicy_CorruptFieldIsInternal reads a stored row that no
// longer parses. The parsers it calls answer KindInvalidArgument,
// because the same functions parse what a caller submits; a value that
// reached the column passed that boundary already, so a row failing
// here is this side's corruption and not the caller's request. The
// control is the row that parses, which stops being accepted if the
// conversion refuses for any reason other than the corrupted field.
func TestRowToPolicy_CorruptFieldIsInternal(t *testing.T) {
	ok, err := rowToPolicy(policyRow())
	require.NoError(t, err, "the row that parses is the control")
	require.Equal(t, "read-only", ok.Name)

	corruptions := map[string]func(*db.Policy){
		"grn":     func(row *db.Policy) { row.Grn = corrupt },
		"org grn": func(row *db.Policy) { row.OrgGrn = corrupt },
		"status":  func(row *db.Policy) { row.Status = "retired" },
	}
	for name, corruptField := range corruptions {
		t.Run(name, func(t *testing.T) {
			row := policyRow()
			corruptField(&row)
			_, err := rowToPolicy(row)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v: %v", errorx.KindOf(err), err)
		})
	}
}

// TestRowToPolicyVersion_CorruptFieldIsInternal is
// TestRowToPolicy_CorruptFieldIsInternal for the version row.
func TestRowToPolicyVersion_CorruptFieldIsInternal(t *testing.T) {
	ok, err := rowToPolicyVersion(policyVersionRow())
	require.NoError(t, err, "the row that parses is the control")
	require.Equal(t, 1, ok.Version)

	corruptions := map[string]func(*db.PolicyVersion){
		"policy grn": func(row *db.PolicyVersion) { row.PolicyGrn = corrupt },
		"author":     func(row *db.PolicyVersion) { row.CreatedBy = corrupt },
		"document":   func(row *db.PolicyVersion) { row.Document = []byte(`{"Statement":[]}`) },
	}
	for name, corruptField := range corruptions {
		t.Run(name, func(t *testing.T) {
			row := policyVersionRow()
			corruptField(&row)
			_, err := rowToPolicyVersion(row)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v: %v", errorx.KindOf(err), err)
		})
	}
}

// TestStoredRowParsers_AnswerTheCallersKindOnTheirOwn isolates what the
// two tests above measure: the parsers a row conversion calls carry
// KindInvalidArgument, so the wrap is the only thing that can make a
// corrupt row internal.
func TestStoredRowParsers_AnswerTheCallersKindOnTheirOwn(t *testing.T) {
	_, err := grn.Parse(corrupt)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "grn.Parse: %v", err)

	_, err = authz.ParsePolicyStatus("retired")
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "ParsePolicyStatus: %v", err)

	_, err = authz.ParsePolicyDocument([]byte(`{"Statement":[]}`))
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "ParsePolicyDocument: %v", err)
}
