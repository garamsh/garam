package audit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/internal/audit"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// TestCredential_IsClosedOverItsOutcome holds the invariant the event
// keeps: an issued one carries what was issued and no reason, and a
// refused one a reason and nothing issued. Each constructor is the
// other's control.
func TestCredential_IsClosedOverItsOutcome(t *testing.T) {
	subject := grn.New("acme", grn.DefaultProject, "operator", "one")
	v := audit.Validity{NotBefore: time.Unix(100, 0), NotAfter: time.Unix(200, 0)}

	issued := audit.CredentialIssued(subject, audit.CredentialOperatorCertificate, audit.ActionRenew, "42", v, true)
	assert.Equal(t, audit.OutcomeIssued, issued.Outcome())
	assert.Equal(t, "42", issued.Serial())
	assert.Equal(t, v, issued.Validity())
	assert.True(t, issued.Replay())
	_, ok := issued.Reason()
	assert.False(t, ok, "an issued event names no reason")

	refused := audit.CredentialRefused(subject, audit.CredentialOperatorCertificate, audit.ActionRenew, errorx.KindFailedPrecondition)
	assert.Equal(t, audit.OutcomeRefused, refused.Outcome())
	reason, ok := refused.Reason()
	assert.True(t, ok)
	assert.Equal(t, "failed_precondition", reason, "the kind's name, never the failure's text")
	assert.Empty(t, refused.Serial())
	assert.Equal(t, audit.Validity{}, refused.Validity())
	assert.False(t, refused.Replay())
}
