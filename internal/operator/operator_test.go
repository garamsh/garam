package operator_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

func acmeGRN() grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")
}

func aliceGRN() grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "user", "alice")
}

func TestNew_BuildsAnOrgLevelOperatorGRN(t *testing.T) {
	o, err := operator.New(acmeGRN(), "field-one", aliceGRN(), operator.DetailsPatch{})
	require.NoError(t, err)

	assert.Equal(t, "grn:acme:default:operator:field-one", o.GRN().String())
	assert.Equal(t, acmeGRN(), o.Organization())
	assert.Equal(t, aliceGRN(), o.RegisteredBy())
	assert.False(t, o.CreatedAt().IsZero())
}

// TestNew_RefusesAnIdentifierThatIsNotASegment holds the character rule
// at the boundary. A colon is the case that matters: Parse splits on
// every one, so an identifier carrying one names a different resource
// than the one written (grn-model.md §identifier).
//
// The accepted identifier is the control: each refusal below stops
// holding for it, so what refuses is the rule and not the constructor.
func TestNew_RefusesAnIdentifierThatIsNotASegment(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
	}{
		{"empty", ""},
		{"a colon, which re-associates the segments", "field:one"},
		{"uppercase", "Field-One"},
		{"an underscore", "field_one"},
		{"past the segment length", string(make([]byte, 65))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := operator.New(acmeGRN(), tt.identifier, aliceGRN(), operator.DetailsPatch{})
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}

	_, err := operator.New(acmeGRN(), "field-one", aliceGRN(), operator.DetailsPatch{})
	assert.NoError(t, err)
}

// TestNew_StartsPending covers the state a record carries before
// anything is stored. Registering writes the record and mints the token
// in one write (ADR-0064), so a registration never exists without a
// live token beside it and `pending` is what that is.
//
// A certificate already recorded for the GRN is the case this value
// cannot see, which is why the store answers the registration rather
// than this: the coverage for that is in tests/integration.
func TestNew_StartsPending(t *testing.T) {
	o, err := operator.New(acmeGRN(), "field-one", aliceGRN(), operator.DetailsPatch{})
	require.NoError(t, err)

	assert.Equal(t, operator.EnrollmentStatePending, o.EnrollmentState())
	assert.True(t, o.CertificateIssuedAt().IsZero(),
		"garam has issued no certificate for an operator it has not registered")
}

// TestNew_CallsAnOperatorItsIdentifierUntilItIsNamed covers the name a
// registration starts with (ADR-0074). A registration that names none
// is called by its identifier, which is what the console showed every
// operator by before names existed. The named registration is the
// control: without it the identifier would be the answer for a
// constructor that ignores what it is given.
func TestNew_CallsAnOperatorItsIdentifierUntilItIsNamed(t *testing.T) {
	unnamed, err := operator.New(acmeGRN(), "field-one", aliceGRN(), operator.DetailsPatch{})
	require.NoError(t, err)
	assert.Equal(t, "field-one", unnamed.Name())
	assert.Empty(t, unnamed.Description())

	details, err := operator.NewDetailsPatch(ptr("Field cluster one"), ptr("Runs the batch agents."))
	require.NoError(t, err)
	named, err := operator.New(acmeGRN(), "field-one", aliceGRN(), details)
	require.NoError(t, err)
	assert.Equal(t, "Field cluster one", named.Name())
	assert.Equal(t, "Runs the batch agents.", named.Description())
	assert.Equal(t, unnamed.GRN(), named.GRN(), "a name is not part of the identity")
}

// TestNewDetailsPatch_RefusesWhatItCannotStore holds the bounds
// migrations/000025 also holds, and the text PostgreSQL refuses on its
// own. The bounds count characters rather than bytes, so the accepted
// cases at each bound are written in a script whose characters take
// three bytes each: a byte count would refuse them, and they are the
// control that tells the two apart.
func TestNewDetailsPatch_RefusesWhatItCannotStore(t *testing.T) {
	tests := []struct {
		name        string
		opName      *string
		description *string
	}{
		{"an empty name", ptr(""), nil},
		{"a name that is only space", ptr("   "), nil},
		{"a name past 100 characters", ptr(strings.Repeat("가", operator.NameMaxLength+1)), nil},
		{"a description past 1000 characters", nil, ptr(strings.Repeat("가", operator.DescriptionMaxLength+1))},
		{"a name holding NUL", ptr("a\x00b"), nil},
		{"a description holding NUL", nil, ptr("a\x00b")},
		{"a name that is not UTF-8", ptr("a\xffb"), nil},
		{"a description that is not UTF-8", nil, ptr("a\xffb")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := operator.NewDetailsPatch(tt.opName, tt.description)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}

	p, err := operator.NewDetailsPatch(
		ptr(strings.Repeat("가", operator.NameMaxLength)),
		ptr(strings.Repeat("가", operator.DescriptionMaxLength)))
	require.NoError(t, err)
	assert.Equal(t, operator.NameMaxLength, utf8.RuneCountInString(*p.Name()))

	// The control for the NUL and UTF-8 cases: the same text without
	// the offending byte is taken.
	_, err = operator.NewDetailsPatch(ptr("ab"), ptr("ab"))
	assert.NoError(t, err)
}

// TestNewDetailsPatch_TrimsAndKeepsWhatItWasNotGiven covers the two
// shapes a patch carries besides a refusal: a field given is trimmed,
// and a field left out stays absent rather than becoming empty, which
// is what lets an update change one field and leave the other. An
// empty description is given, and clears the one there was.
func TestNewDetailsPatch_TrimsAndKeepsWhatItWasNotGiven(t *testing.T) {
	p, err := operator.NewDetailsPatch(ptr("  Field one  "), nil)
	require.NoError(t, err)
	require.NotNil(t, p.Name())
	assert.Equal(t, "Field one", *p.Name())
	assert.Nil(t, p.Description())

	p, err = operator.NewDetailsPatch(nil, ptr(""))
	require.NoError(t, err)
	assert.Nil(t, p.Name())
	require.NotNil(t, p.Description())
	assert.Empty(t, *p.Description())
}

func ptr(s string) *string { return &s }
