package wire_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// TestPolicyStatusRoundTrip renders each status and reads it back, and
// holds the two fallbacks against it: a status outside the constants
// renders as unspecified, and unspecified reads back as the zero value.
func TestPolicyStatusRoundTrip(t *testing.T) {
	statuses := []struct {
		status wire.PolicyStatus
		proto  authzv1.PolicyStatus
	}{
		{wire.PolicyStatusDraft, authzv1.PolicyStatus_POLICY_STATUS_DRAFT},
		{wire.PolicyStatusActive, authzv1.PolicyStatus_POLICY_STATUS_ACTIVE},
	}
	for _, c := range statuses {
		t.Run(string(c.status), func(t *testing.T) {
			assert.Equal(t, c.proto, wire.PolicyStatusToProto(c.status))
			assert.Equal(t, c.status, wire.PolicyStatusFromProto(c.proto))
		})
	}

	assert.Equal(t, authzv1.PolicyStatus_POLICY_STATUS_UNSPECIFIED, wire.PolicyStatusToProto("retired"))
	assert.Equal(t, wire.PolicyStatus(""), wire.PolicyStatusFromProto(authzv1.PolicyStatus_POLICY_STATUS_UNSPECIFIED))
}
