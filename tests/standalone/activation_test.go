package standalone_test

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStandalone_ReplaysALostActivationAndRefusesAnEndedOne holds the
// activation's identity across loss and restart on a real garam (T9,
// T10).
//
// T9: garam commits the first activation and its answer is lost, by
// the test's interposer; the supervisor is then stopped and started
// again. The adapter's retry reaches the new supervisor, which sends
// garam the same request identifier, and garam answers the same
// activation with a higher token version: one activation exists.
//
// T10: after a replacement, the ended intent's own request sent to
// garam again is refused with 409, and neither the supervisor's anchor
// nor garam's current activation moves. The control is the live
// intent's request sent again, which garam answers with the same
// activation and a higher token version.
func TestStandalone_ReplaysALostActivationAndRefusesAnEndedOne(t *testing.T) {
	s := newStack(t)
	h := s.newHost("agent")
	h.prepare()
	h.approve(time.Now().Add(time.Hour))
	out, err := h.enroll()
	require.NoError(t, err, out)
	agentGRN, _, epoch := h.enrolled()
	certPEM, keyPEM := s.serving(net.IPv4(127, 0, 0, 1))
	p := s.interpose(h, certPEM, keyPEM)
	p.lose = true

	first := h.run(runOptions{machineURL: p.url})
	s.eventually("garam committing the first activation", func() bool { return len(s.activations(agentGRN)) == 1 })
	committed := s.activations(agentGRN)[0]
	require.EqualValues(t, 1, committed.TokenVersion, "the answer to the first token was lost")
	require.NoError(t, first.stop(), first.out.String())

	second := h.run(runOptions{machineURL: p.url})
	p.release()
	s.eventually("the replayed activation", func() bool {
		a := s.activations(agentGRN)
		return len(a) == 1 && a[0].TokenVersion >= 2
	})
	got := s.activations(agentGRN)
	require.Len(t, got, 1, "one activation")
	assert.Equal(t, committed.ID, got[0].ID, "the same activation")
	asked := p.asked()
	require.GreaterOrEqual(t, len(asked), 2, "the first request and its retry")
	for _, a := range asked {
		assert.Equal(t, asked[0], a, "every attempt is the first request, unchanged")
	}
	before := h.record()
	assert.Equal(t, committed.RequestID, before.Intent.RequestID)
	assert.Equal(t, asked[0]["requestId"], before.Intent.RequestID)
	assert.Equal(t, committed.ID, before.LastActivation)
	require.NoError(t, second.stop(), second.out.String())

	h.run(runOptions{extra: []string{"--replace"}})
	s.eventually("the successor activated", func() bool {
		a := s.activations(agentGRN)
		return len(a) == 2 && a[1].Ended == ""
	})
	after := h.record()
	require.NotEqual(t, before.Intent.ID, after.Intent.ID)
	assert.Equal(t, committed.ID, after.Intent.Replaces, "the successor names its predecessor's activation")
	assert.Equal(t, "superseded", s.activations(agentGRN)[0].Ended)

	leaf, err := os.ReadFile(filepath.Join(h.agent, "agent.crt"))
	require.NoError(t, err)
	replay := func(r record) (int, map[string]any) {
		var replaces any
		if r.Intent.Replaces != "" {
			replaces = r.Intent.Replaces
		}
		status, raw := h.call(h.supervisorClient(), "/agents/"+agentGRN+"/activations", "execution-fence.v1", "",
			map[string]any{
				"requestId": r.Intent.RequestID, "epoch": epoch, "generation": r.Intent.Generation,
				"replacesActivationId": replaces, "operationRef": nil, "certificatePem": string(leaf),
			})
		var answer map[string]any
		_ = json.Unmarshal(raw, &answer)
		return status, answer
	}
	current := s.activations(agentGRN)[1]
	status, answer := replay(before)
	assert.Equal(t, http.StatusConflict, status, "the ended intent's request is refused: %v", answer)
	t.Logf("the ended intent's replay answered %d %v", status, answer)
	assert.Equal(t, after.LastActivation, h.record().LastActivation, "the supervisor's anchor does not move")
	now := s.activations(agentGRN)
	require.Len(t, now, 2, "no activation is created")
	assert.Equal(t, current, now[1], "garam's current activation does not move")

	status, answer = replay(after)
	require.Equal(t, http.StatusOK, status, "the control: the live intent's request is answered again: %v", answer)
	assert.Equal(t, current.ID, answer["activationId"])
	assert.Greater(t, s.activations(agentGRN)[1].TokenVersion, current.TokenVersion)
}
