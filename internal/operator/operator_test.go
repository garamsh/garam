package operator_test

import (
	"testing"

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
	o, err := operator.New(acmeGRN(), "field-one", aliceGRN())
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
			_, err := operator.New(acmeGRN(), tt.identifier, aliceGRN())
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}

	_, err := operator.New(acmeGRN(), "field-one", aliceGRN())
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
	o, err := operator.New(acmeGRN(), "field-one", aliceGRN())
	require.NoError(t, err)

	assert.Equal(t, operator.EnrollmentStatePending, o.EnrollmentState())
	assert.True(t, o.CertificateIssuedAt().IsZero(),
		"garam has issued no certificate for an operator it has not registered")
}
