package delivery_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/platform/internal/delivery"
)

// binding is the binding the fake runtime reports.
func binding() delivery.Binding {
	return delivery.Binding{Agent: agentGRN(), Epoch: "3", Generation: strings.Repeat("a", 32), Revision: "7"}
}

// TestRequestID_IsOneIdentifierPerIntent pins ADR-0084's derivation: a
// version 8 UUID that is the same for the same binding and placement,
// whatever process computes it, and differs when any of its inputs
// does. The vector is what a second implementation is checked against.
func TestRequestID_IsOneIdentifierPerIntent(t *testing.T) {
	id := delivery.RequestID(delivery.TokenExecutionContract, binding(), "placement-1")
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), id)
	assert.Equal(t, "adc5649c-475f-8945-a46d-bca4be49eee6", id,
		"agent-execution.v2 is the first field, so the same binding is new intent (ADR-0100); computed apart from this code")
	assert.Equal(t, id, delivery.RequestID(delivery.TokenExecutionContract, binding(), "placement-1"), "the same intent is the same request")
	assert.NotContains(t, id, "placement-1")
	assert.Equal(t, "4e0aafc0-27d8-8037-ba34-d8d62580a6e5",
		delivery.RequestID("agent-execution.v1", binding(), "placement-1"),
		"ADR-0084's derivation over the name in ADR-0142's grammar, computed apart from this code; the ADR's own vector, 737a3319-e377-8fab-ba8c-fe01762adb62, is over the former name")

	changes := map[string]func(b *delivery.Binding, token *string){
		"another agent":      func(b *delivery.Binding, _ *string) { b.Agent = senderGRN() },
		"another epoch":      func(b *delivery.Binding, _ *string) { b.Epoch = "4" },
		"another generation": func(b *delivery.Binding, _ *string) { b.Generation = strings.Repeat("b", 32) },
		"another revision":   func(b *delivery.Binding, _ *string) { b.Revision = "8" },
		"another placement":  func(_ *delivery.Binding, token *string) { *token = "placement-2" },
		"a field boundary moved": func(b *delivery.Binding, _ *string) {
			b.Epoch, b.Revision = "37", ""
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			b, token := binding(), "placement-1"
			change(&b, &token)
			assert.NotEqual(t, id, delivery.RequestID(delivery.TokenExecutionContract, b, token))
		})
	}
}
