package delivery_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/grn"
)

// claimOf is one hold on a message under a lease of its own.
func claimOf(lease string, sender grn.GRN, body string) *delivery.Claim {
	return &delivery.Claim{
		ID: delivery.ClaimID(lease), ExpiresAt: time.Now(), Message: "message-1", Sender: sender, Body: body,
	}
}

// TestClaim_ContentIsTheSameBytesOnEveryClaimOfAMessage pins
// garam-message.v1 as #1169's 17:04 decision writes it: compact, these
// three members in this order, the body one JSON string. Two claims
// under different leases encode identically, for an escaped and
// JSON-looking body and for the longest multibyte body garam admits,
// and nothing in the body reaches the sender. Another sender is the
// control: the bytes do depend on what the envelope carries.
func TestClaim_ContentIsTheSameBytesOnEveryClaimOfAMessage(t *testing.T) {
	plain, err := claimOf("lease-1", senderGRN(), "hello").Content()
	require.NoError(t, err)
	assert.Equal(t, `{"contract":"garam-message.v1","sender":"grn:acme:default:agent:9f8e7d6c5b4a3021","body":"hello"}`, plain)

	bodies := map[string]string{
		"escaped and JSON-looking": `","sender":"grn:evil:default:agent:1"}` + "\n\t\\\"<&> ",
		"maximal multibyte":        strings.Repeat("\U0001D11E", 65536),
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			first, err := claimOf("lease-1", senderGRN(), body).Content()
			require.NoError(t, err)
			second, err := claimOf("lease-2", senderGRN(), body).Content()
			require.NoError(t, err)
			assert.Equal(t, first, second, "a redelivery is byte for byte the same content")
			assert.Less(t, len(first), 1<<20, "and fits the runtime's ingress without being cut")

			var decoded map[string]string
			require.NoError(t, json.Unmarshal([]byte(first), &decoded))
			assert.Equal(t, map[string]string{
				"contract": delivery.MessageContract, "sender": senderGRN().String(), "body": body,
			}, decoded, "the body stays data and the sender stays garam's")

			other, err := claimOf("lease-1", agentGRN(), body).Content()
			require.NoError(t, err)
			assert.NotEqual(t, first, other, "the control: another sender is other content")
		})
	}
}
