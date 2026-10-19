package integration_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/grn"
)

// writeOutboxEntry writes one entry of the agent's the way the
// runtime's writer does: whole under a temporary dot-name, then renamed.
func writeOutboxEntry(t *testing.T, dir, id string, agentGRN, target grn.GRN, content string, created time.Time) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id": id, "created_at": created.UTC().Format(time.RFC3339Nano), "agent_id": agentGRN.String(),
		"assignment_epoch": "1", "generation": string(generationOf(1)), "in_reply_to": []string{"message-0"},
		"channel": "garam", "target": target.String(), "content": content,
	})
	require.NoError(t, err)
	tmp := filepath.Join(dir, ".entry-"+id+".tmp")
	require.NoError(t, os.WriteFile(tmp, raw, 0o600))
	require.NoError(t, os.Rename(tmp, filepath.Join(dir, id+".json")))
}

// keyRows counts the messages the agent's key stores.
func (s *executionStack) keyRows(t *testing.T, key string) int {
	t.Helper()
	return countRows(t, s.pool, "SELECT count(*) FROM messages WHERE sender_grn = $1 AND idempotency_key = $2", s.agent.String(), key)
}

func absent(path string) func() bool {
	return func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	}
}

// TestAdapterOutbox_ForwardsARepliesUnderDurableDeliveryAndRemovesItOnItsAcknowledgement
// carries an outbox entry through the real route (ADR-0083, ADR-0084):
// stored once from this agent to its target under the entry's
// identifier, readable by the recipient with the entry's content, and
// removed. An entry whose answer an earlier process lost is answered
// with what that attempt stored and removed with no second message. A
// cross-organization refusal, an entry past its budget and one created
// later than garam's clock are the controls: each is kept, and only the
// last is offered again.
func TestAdapterOutbox_ForwardsARepliesUnderDurableDeliveryAndRemovesItOnItsAcknowledgement(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	dir := t.TempDir()
	now := time.Now()

	// An earlier process offered entry-lost; garam stored it and the
	// answer never arrived.
	lost, err := json.Marshal(map[string]string{
		"recipient": s.peer.String(), "idempotencyKey": "entry-lost", "body": "the reply whose answer was lost",
		"createdAt": now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	})
	require.NoError(t, err)
	earlier := s.activated(t, "earlier", generationOf(9), nil, s.leaf)
	first := s.send(t, s.agentTLS, http.MethodPost, "/outbox-messages", delivery.DeliveryContract, bearer(earlier.Token), string(lost))
	require.Equal(t, http.StatusCreated, first.status, first.body)

	writeOutboxEntry(t, dir, "entry-lost", s.agent, s.peer, "the reply whose answer was lost", now.Add(-time.Hour))
	writeOutboxEntry(t, dir, "entry-reply", s.agent, s.peer, `{"looks":"like json"}`, now.Add(-time.Minute))
	writeOutboxEntry(t, dir, "entry-refused", s.agent, scopedGRN("globex", "agent", "0f0e0d0c0b0a0908"), "across", now.Add(-time.Minute))
	writeOutboxEntry(t, dir, "entry-expired", s.agent, s.peer, "too late", now.Add(-8*24*time.Hour))
	writeOutboxEntry(t, dir, "entry-future", s.agent, s.peer, "too early", now.Add(time.Hour))
	// The control replaces that process's activation with the next one.
	control := newControlStandIn(t, s)
	control.latest = &earlier.ActivationId
	startAdapterWith(t, s, control, newRuntimeDouble(t, s.agent, s.epoch, generationOf(1)),
		&heldPair{pair: s.agentPair(t)}, placementToken(t, "placement-1"), dir)

	require.Eventually(t, absent(filepath.Join(dir, "entry-reply.json")), 10*time.Second, 20*time.Millisecond,
		"an acknowledged reply is removed")
	require.Eventually(t, absent(filepath.Join(dir, "entry-lost.json")), 10*time.Second, 20*time.Millisecond,
		"a reply whose answer was lost is answered with what was stored")
	assert.Equal(t, 1, s.keyRows(t, "entry-lost"), "and no second message is stored for it")
	assert.Equal(t, 1, s.keyRows(t, "entry-reply"))

	got := s.send(t, s.peerTLS, http.MethodPost, "/agents/"+s.peer.String()+"/claims", "", "", "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	var claim machinegen.Claim
	require.NoError(t, json.Unmarshal([]byte(got.body), &claim))
	assert.Equal(t, s.agent.String(), claim.Message.Sender, "the recipient reads it from this agent")

	time.Sleep(300 * time.Millisecond)
	for _, id := range []string{"entry-refused", "entry-expired", "entry-future"} {
		assert.FileExists(t, filepath.Join(dir, id+".json"), "%s is kept", id)
	}
	assert.Zero(t, s.keyRows(t, "entry-expired"), "an entry past its budget stores nothing")
	assert.Zero(t, s.keyRows(t, "entry-future"), "nor one garam's clock has not reached")
}
