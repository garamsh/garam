package operator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// TestParseOperation_NamesActionsTheManifestsDeclare holds each
// operation a member can hand an operator to an action grn.Actions
// holds, with an undeclared name as the control. `iam:agent-activate`
// bounds the hosted operator's own lifecycle authority, no member mints
// it and no manifest declares it, so it is not among them.
func TestParseOperation_NamesActionsTheManifestsDeclare(t *testing.T) {
	declared := map[string]bool{}
	for _, a := range grn.Actions() {
		declared[a.Name] = true
	}

	for _, o := range []operator.Operation{
		operator.OperationAgentCreate, operator.OperationAgentConfigure, operator.OperationAgentRecover,
		operator.OperationTemplateRead, operator.OperationTemplatePublish, operator.OperationProfileRead,
		operator.OperationExecutionRead, operator.OperationOperatorRead, operator.OperationAgentReplace,
	} {
		_, err := operator.ParseOperation(string(o))
		assert.NoError(t, err, string(o))
		assert.True(t, declared[string(o)], string(o))
	}
	assert.False(t, declared["iam:agent-no-such-verb"])
}
