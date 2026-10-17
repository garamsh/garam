package delivery_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/delivery"
)

// createdAt is when every entry here was written, with the variable
// fraction the runtime's writer produces.
var createdAt = time.Date(2026, 10, 5, 1, 2, 3, 456789000, time.UTC)

// entryFor is an outbox entry as the runtime's writer records one: this
// agent's reply to the sender, from an earlier generation.
func entryFor(id, content string) map[string]any {
	return map[string]any{
		"id": id, "created_at": createdAt.Format(time.RFC3339Nano), "agent_id": agentGRN().String(),
		"assignment_epoch": "2", "generation": "0123456789abcdef0123456789abcdef",
		"in_reply_to": []string{"message-0"}, "channel": "garam", "target": senderGRN().String(),
		"content": content,
	}
}

// writeEntry writes an entry the way the runtime's writer does: under a
// temporary dot-name first, renamed to its identifier once whole.
func writeEntry(t *testing.T, dir string, entry map[string]any) {
	t.Helper()
	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	tmp := filepath.Join(dir, ".entry-"+entry["id"].(string)+".tmp")
	require.NoError(t, os.WriteFile(tmp, raw, 0o600))
	require.NoError(t, os.Rename(tmp, filepath.Join(dir, entry["id"].(string)+".json")))
}

// forwarding runs a legacy loop forwarding dir's outbox.
func forwarding(t *testing.T, queue *garamQueue, dir string) func() {
	t.Helper()
	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)
	return start(t, legacyEnds(t, queue, newAgentGateway(t), outbox), brief())
}

func gone(path string) func() bool {
	return func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	}
}

// TestRun_ForwardsAReplyAndRemovesItOnAnAcknowledgementOfIt is the
// durable reply as ADR-0079 states it: offered under its own identifier
// as the key, with its creation instant, and removed once garam answers
// with a message stored from this agent to its target under that key. A
// cross-organization refusal is answered 201 too, from the target back
// to this agent: that entry is the control, kept and offered once.
func TestRun_ForwardsAReplyAndRemovesItOnAnAcknowledgementOfIt(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	queue.answerOutbox(func(s outboxSubmission) (int, string, any) {
		stored := machinegen.Message{
			Id: "stored-" + s.IdempotencyKey, Sender: agentGRN().String(), Recipient: s.Recipient,
			IdempotencyKey: s.IdempotencyKey,
		}
		if s.IdempotencyKey == "entry-refused" {
			stored = machinegen.Message{
				Id: "refusal-1", Sender: s.Recipient, Recipient: agentGRN().String(),
				IdempotencyKey: "refusal:" + s.IdempotencyKey,
			}
		}
		return http.StatusCreated, delivery.DeliveryContract, stored
	})
	dir := t.TempDir()
	writeEntry(t, dir, entryFor("entry-refused", "across the boundary"))
	writeEntry(t, dir, entryFor("entry-1", `{"looks":"like json"}`))
	forwarding(t, queue, dir)

	eventually(t, gone(filepath.Join(dir, "entry-1.json")), "an acknowledged reply is removed")
	quietFor()
	assert.FileExists(t, filepath.Join(dir, "entry-refused.json"), "a refusal stored in its place keeps the entry")

	byKey := map[string][]outboxSubmission{}
	for _, s := range queue.submissions() {
		byKey[s.IdempotencyKey] = append(byKey[s.IdempotencyKey], s)
	}
	require.Len(t, byKey["entry-1"], 1)
	sent := byKey["entry-1"][0]
	assert.Equal(t, senderGRN().String(), sent.Recipient, "the target is the recipient")
	assert.Equal(t, `{"looks":"like json"}`, sent.Body, "the content is the body, unchanged")
	assert.True(t, createdAt.Equal(sent.CreatedAt), "the entry's creation instant, not a new one")
	assert.Len(t, byKey["entry-refused"], 1, "a refused entry is offered once, not in a loop")
	assert.Zero(t, queue.legacyPosts(), "nothing goes to POST /messages")
}

// TestRun_KeepsAnEntryGaramRefusesOnItsMeritsAndOffersItNoMore covers
// the definite answers durable-delivery.v1 pins on an entry: an expired
// entry (410) and a conflicting key (409). Each keeps the entry, is
// offered once, and never falls back to POST /messages. The controls
// are kept too, and offered again: an entry garam's clock has not
// reached (400 out_of_range), and one a peer not serving the contract
// answered, a server without the route (404) or one answering outside
// it (ADR-0108).
func TestRun_KeepsAnEntryGaramRefusesOnItsMeritsAndOffersItNoMore(t *testing.T) {
	answers := map[string]func() (int, string, any){
		"entry-old-server": func() (int, string, any) {
			return http.StatusNotFound, "", machinegen.Error{Kind: "not_found", Message: "no route"}
		},
		"entry-unversioned": func() (int, string, any) {
			return http.StatusCreated, "", machinegen.Message{Id: "x"}
		},
		"entry-expired": func() (int, string, any) {
			return http.StatusGone, delivery.DeliveryContract, machinegen.Error{Kind: "deadline_exceeded", Message: "expired"}
		},
		"entry-conflict": func() (int, string, any) {
			return http.StatusConflict, delivery.DeliveryContract, machinegen.Error{Kind: "already_exists", Message: "another request"}
		},
		"entry-future": func() (int, string, any) {
			return http.StatusBadRequest, delivery.DeliveryContract, machinegen.Error{Kind: "out_of_range", Message: "future"}
		},
	}
	queue := newGaramQueue(t, time.Second)
	queue.answerOutbox(func(s outboxSubmission) (int, string, any) { return answers[s.IdempotencyKey]() })
	dir := t.TempDir()
	for id := range answers {
		writeEntry(t, dir, entryFor(id, "reply"))
	}
	forwarding(t, queue, dir)

	offeredAgain := []string{"entry-future", "entry-old-server", "entry-unversioned"}
	eventually(t, func() bool {
		for _, id := range offeredAgain {
			if count(queue.submissions(), id) < 3 {
				return false
			}
		}
		return true
	}, "the controls: an entry the clock or the peer was not ready for is offered again")
	for id := range answers {
		assert.FileExists(t, filepath.Join(dir, id+".json"), "%s is kept", id)
	}
	for _, id := range []string{"entry-expired", "entry-conflict"} {
		assert.Equal(t, 1, count(queue.submissions(), id), "%s is offered once", id)
	}
	assert.Zero(t, queue.legacyPosts(), "no answer falls back to POST /messages")
}

// TestRun_ForwardsAReplyOnceGaramServesItsContractWithoutARestart is
// an adapter upgraded before garam: the reply is answered outside
// durable-delivery.v1 until garam serves it, and the same process then
// forwards it under the same key and creation instant (ADR-0108).
func TestRun_ForwardsAReplyOnceGaramServesItsContractWithoutARestart(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	unserved := 3
	queue.answerOutbox(func(s outboxSubmission) (int, string, any) {
		if unserved > 0 {
			unserved--
			return http.StatusNotFound, "", machinegen.Error{Kind: "not_found", Message: "no route"}
		}
		return http.StatusCreated, delivery.DeliveryContract, machinegen.Message{
			Id: "stored-1", Sender: agentGRN().String(), Recipient: s.Recipient, IdempotencyKey: s.IdempotencyKey,
		}
	})
	dir := t.TempDir()
	writeEntry(t, dir, entryFor("entry-1", "reply"))
	forwarding(t, queue, dir)

	eventually(t, gone(filepath.Join(dir, "entry-1.json")), "the reply goes once garam serves the contract")
	submissions := queue.submissions()
	require.Len(t, submissions, 4, "three offers answered outside the contract and the one acknowledged")
	for _, s := range submissions {
		assert.Equal(t, "entry-1", s.IdempotencyKey)
		assert.True(t, createdAt.Equal(s.CreatedAt))
	}
	assert.Zero(t, queue.legacyPosts(), "nothing falls back to POST /messages")
}

func count(submissions []outboxSubmission, key string) int {
	n := 0
	for _, s := range submissions {
		if s.IdempotencyKey == key {
			n++
		}
	}
	return n
}

// TestRun_RetriesAnUnavailableGaramUnderTheSameIdentity is the outage:
// garam does not answer, the entry is offered again with backoff, and
// every offer carries the same key and creation instant until one is
// acknowledged.
func TestRun_RetriesAnUnavailableGaramUnderTheSameIdentity(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	failures := 3
	queue.answerOutbox(func(s outboxSubmission) (int, string, any) {
		if failures > 0 {
			failures--
			return http.StatusServiceUnavailable, "", machinegen.Error{Kind: "unavailable", Message: "internal error"}
		}
		return http.StatusCreated, delivery.DeliveryContract, machinegen.Message{
			Id: "stored-1", Sender: agentGRN().String(), Recipient: s.Recipient, IdempotencyKey: s.IdempotencyKey,
		}
	})
	dir := t.TempDir()
	writeEntry(t, dir, entryFor("entry-1", "reply"))
	forwarding(t, queue, dir)

	eventually(t, gone(filepath.Join(dir, "entry-1.json")), "the entry goes once garam answers")
	submissions := queue.submissions()
	require.Len(t, submissions, 4, "three unanswered offers and the one acknowledged")
	for _, s := range submissions {
		assert.Equal(t, "entry-1", s.IdempotencyKey)
		assert.True(t, createdAt.Equal(s.CreatedAt))
	}
}

// TestRun_ForwardsAfterARestartAReplyWhoseAcknowledgementWasLost is the
// lost answer and the restart together: garam stored the reply and the
// answer never arrived, so the entry stays, and the next process offers
// it under the same key and is answered with what was stored. One
// message is stored for it.
func TestRun_ForwardsAfterARestartAReplyWhoseAcknowledgementWasLost(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	queue.answerOutbox(func(s outboxSubmission) (int, string, any) {
		// Stored, and the answer lost on the way back. The callback
		// runs under the queue's lock.
		queue.outbox.stored[s.IdempotencyKey] = machinegen.Message{
			Id: "stored-first", Sender: agentGRN().String(), Recipient: s.Recipient, IdempotencyKey: s.IdempotencyKey,
		}
		return http.StatusServiceUnavailable, "", machinegen.Error{Kind: "unavailable", Message: "internal error"}
	})
	dir := t.TempDir()
	writeEntry(t, dir, entryFor("entry-1", "reply"))
	stop := forwarding(t, queue, dir)
	eventually(t, func() bool { return len(queue.submissions()) >= 1 }, "the reply is offered")
	stop()
	assert.FileExists(t, filepath.Join(dir, "entry-1.json"), "an unanswered offer keeps the entry")

	queue.answerOutbox(nil)
	forwarding(t, queue, dir)
	eventually(t, gone(filepath.Join(dir, "entry-1.json")), "the next process's offer is acknowledged")
	for _, s := range queue.submissions() {
		assert.Equal(t, "entry-1", s.IdempotencyKey, "every offer is the same key")
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	assert.Len(t, queue.outbox.stored, 1, "one message is stored for the reply")
	assert.Equal(t, "stored-first", queue.outbox.stored["entry-1"].Id)
}

// TestRun_ReadsAgainAnEntryTheSystemCouldNotOpen is a reply the
// adapter could not open for a reason that is not the entry's — here a
// mode that refuses the reader, standing in for a descriptor limit or
// an I/O error. It is not set aside: once it can be read it is
// forwarded. A planted link beside it is the control: it is set aside
// and never offered.
func TestRun_ReadsAgainAnEntryTheSystemCouldNotOpen(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	dir := t.TempDir()
	writeEntry(t, dir, entryFor("entry-1", "reply"))
	path := filepath.Join(dir, "entry-1.json")
	require.NoError(t, os.Chmod(path, 0o000))
	writeEntry(t, dir, entryFor("entry-target", "reply"))
	require.NoError(t, os.Rename(filepath.Join(dir, "entry-target.json"), filepath.Join(dir, ".target")))
	require.NoError(t, os.Symlink(".target", filepath.Join(dir, "entry-link.json")))
	forwarding(t, queue, dir)

	quietFor()
	require.Empty(t, queue.submissions(), "an entry that cannot be opened is not offered")
	require.NoError(t, os.Chmod(path, 0o600))
	eventually(t, gone(path), "once it can be read it is forwarded")
	assert.Zero(t, count(queue.submissions(), "entry-link"), "the control: a planted link is never offered")
	assert.FileExists(t, filepath.Join(dir, "entry-link.json"))
}

// TestRun_ForwardsPastMoreEntriesSetAsideThanOnePassTakes is the bound
// on a pass meeting an outbox whose oldest entries are all set aside:
// they are passed over before the bound is applied, so the reply after
// them still goes. The set-aside entries are the control: each is kept
// and offered once at most.
func TestRun_ForwardsPastMoreEntriesSetAsideThanOnePassTakes(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	dir := t.TempDir()
	for i := 0; i < 300; i++ {
		undated := entryFor(fmt.Sprintf("entry-a%03d", i), "reply")
		delete(undated, "created_at")
		writeEntry(t, dir, undated)
	}
	writeEntry(t, dir, entryFor("entry-z", "the reply after them"))
	forwarding(t, queue, dir)

	eventually(t, gone(filepath.Join(dir, "entry-z.json")), "the reply past the set-aside entries is forwarded")
	assert.Len(t, queue.submissions(), 1, "the control: no set-aside entry was offered")
	assert.FileExists(t, filepath.Join(dir, "entry-a299.json"))
}

// TestRun_NeverOffersAnEntryItCannotDateOrIsNotItsToSend holds the
// forwarder to what it can offer: an entry with no creation time, one
// of another agent, and one for another channel are kept and never
// offered. The dated entry of this agent beside them is the control.
func TestRun_NeverOffersAnEntryItCannotDateOrIsNotItsToSend(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	dir := t.TempDir()
	undated := entryFor("entry-undated", "reply")
	delete(undated, "created_at")
	writeEntry(t, dir, undated)
	zero := entryFor("entry-zero", "reply")
	zero["created_at"] = "0001-01-01T00:00:00Z"
	writeEntry(t, dir, zero)
	another := entryFor("entry-another", "reply")
	another["agent_id"] = senderGRN().String()
	writeEntry(t, dir, another)
	channel := entryFor("entry-channel", "reply")
	channel["channel"] = "slack"
	writeEntry(t, dir, channel)
	writeEntry(t, dir, entryFor("entry-dated", "reply"))
	forwarding(t, queue, dir)

	eventually(t, gone(filepath.Join(dir, "entry-dated.json")), "the control: a dated entry of this agent is forwarded")
	quietFor()
	for _, id := range []string{"entry-undated", "entry-zero", "entry-another", "entry-channel"} {
		assert.FileExists(t, filepath.Join(dir, id+".json"), "%s is kept", id)
		assert.Zero(t, count(queue.submissions(), id), "%s is never offered", id)
	}
}
