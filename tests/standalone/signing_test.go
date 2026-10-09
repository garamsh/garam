package standalone_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stackIssuer is the issuer the stack's garam signs as
// (GARAM_MACHINE_ISSUER in stack_test.go).
const stackIssuer = "https://garam.test"

// signingConfig is a runtime configuration with Sherlock's offline mock
// model that refuses every unsigned message, so a message the runtime
// takes is one whose signature it verified.
const signingConfig = "revision: r1\nrequire-signature: true\nmodel:\n  provider: mock\n"

// TestStandalone_SignsWhereTheHostNamesGaramsIssuer holds #1312 end to
// end against Sherlock v0.5.0: a host naming garam's issuer has its
// runtime trust garam's key set and its adapter sign, so a message
// reaches a runtime that refuses anything unsigned, and its turn
// completes. The control is a second host naming an issuer garam does
// not sign as: its adapter signs too, its runtime answers 403
// "signature rejected: issuer", and the message is handed out again
// rather than delivered, so what delivered the first was the signature
// verifying and not a runtime taking messages unchecked.
func TestStandalone_SignsWhereTheHostNamesGaramsIssuer(t *testing.T) {
	s := newStack(t)
	from := s.newSender()
	enrolled := func(name string) (*host, string) {
		h := s.newHost(name)
		h.prepare()
		h.approve(time.Now().Add(time.Hour))
		out, err := h.enroll()
		require.NoError(t, err, out)
		agentGRN, _, _ := h.enrolled()
		return h, agentGRN
	}
	runWith := func(h *host, agentGRN, issuer string) *running {
		r := h.run(runOptions{config: s.file(h.name+"-signing.yaml", signingConfig), extra: []string{"--message-issuer", issuer}})
		t.Cleanup(func() {
			if !t.Failed() {
				return
			}
			t.Logf("%s supervisor:\n%s", h.name, s.scrub(tail(r.out.String(), 40)))
			for _, role := range []string{"runtime", "adapter"} {
				if id := h.container(role); id != "" {
					logs, _ := s.docker("logs", id)
					t.Logf("%s %s logs:\n%s", h.name, role, s.scrub(tail(logs, 40)))
				}
			}
		})
		s.eventually(h.name+"'s generation activated", func() bool {
			return len(s.activations(agentGRN)) == 1
		})
		return r
	}

	signed, signedGRN := enrolled("signed")
	runWith(signed, signedGRN, stackIssuer)
	adapterEnv := s.mustDocker("inspect", "-f", "{{json .Config.Env}}", signed.container("adapter"))
	require.Contains(t, adapterEnv, "GARAM_ADAPTER_SIGN_MESSAGES=true", "the setup turned the adapter's signing on")

	from.post(signedGRN, "garam-1312-signed", "hello, signed")
	delivered := s.awaitCompleted(from.grn, "garam-1312-signed")
	assert.Equal(t, signedGRN, delivered.Recipient)

	stranger, strangerGRN := enrolled("stranger")
	runWith(stranger, strangerGRN, "https://not-garam.test")
	from.post(strangerGRN, "garam-1312-stranger", "hello, stranger")
	s.eventually("the control's message handed out again", func() bool {
		got := s.messages("sender_grn = $1 AND idempotency_key = $2", from.grn, "garam-1312-stranger")
		return len(got) == 1 && got[0].Attempts >= 2
	})
	got := s.messages("sender_grn = $1 AND idempotency_key = $2", from.grn, "garam-1312-stranger")
	require.Len(t, got, 1)
	assert.False(t, got[0].Completed, "the control: a runtime trusting another issuer delivered garam's message")
	logs, _ := s.docker("logs", stranger.container("adapter"))
	assert.Contains(t, logs, "signature rejected: issuer", "the control's runtime refused the signature's issuer, not something else")
}
