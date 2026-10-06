package standalone_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStandalone_MovesMemoryThroughAVerifiedHandover is #1202's move on
// a real garam with a real model, through the shipped `garam standalone
// run --move-memory-to`: a fact told before the move is recalled after
// it, by the same agent, in a new generation on a new volume. Garam
// refuses the old generation's token from the switch on, and a message
// sent after the switch can be held only under the new activation,
// however often garam delivers it; each answer its turn sends reaches
// garam once. The controls are the old token admitted before the
// switch and the new token admitted after it.
//
// The test's own acts: the interposer, which keeps the tokens garam
// answers so the test can present the old one as its generation would;
// and the sender, as in the joint scenario.
func TestStandalone_MovesMemoryThroughAVerifiedHandover(t *testing.T) {
	s := newStack(t)
	modelEnv := required(t, "GARAM_TEST_MODEL_ENV_FILE")
	s.secretsFrom(modelEnv)
	tei := s.embedding()

	h := s.newHost("agent")
	h.prepare()
	h.approve(time.Now().Add(time.Hour))
	out, err := h.enroll()
	require.NoError(t, err, out)
	agentGRN, _, _ := h.enrolled()
	from := s.newSender()
	certPEM, keyPEM := s.serving(net.IPv4(127, 0, 0, 1))
	p := s.interpose(h, certPEM, keyPEM)

	config := s.file("config.yaml", fmt.Sprintf(`revision: r1-move
model:
  provider: openai-compatible
  base-url: https://api.minimax.io/v1
  model: MiniMax-M2
  api-key-env: %s
embedding:
  base-url: http://%s:80/v1
  model: BAAI/bge-base-en-v1.5
`, keyName(t, modelEnv), tei))
	extra := []string{"--instructions-file", s.file("instructions.md", replyInstruction)}
	var runs []*running
	start := func(more ...string) *running {
		r := h.run(runOptions{machineURL: p.url, config: config, modelEnv: modelEnv,
			extra: append(append([]string{}, extra...), more...)})
		runs = append(runs, r)
		return r
	}
	t.Cleanup(func() {
		if t.Failed() {
			for i, r := range runs {
				t.Logf("supervisor run %d:\n%s", i, s.scrub(tail(r.out.String(), 40)))
			}
			for _, role := range []string{"runtime", "adapter"} {
				if id := h.container(role); id != "" {
					logs, _ := s.docker("logs", id)
					var turn []string
					for _, line := range strings.Split(logs, "\n") {
						if !strings.Contains(line, "/status\"") {
							turn = append(turn, line)
						}
					}
					t.Logf("%s logs:\n%s", role, s.scrub(tail(strings.Join(turn, "\n"), 60)))
				}
			}
		}
	})

	first := start()
	s.eventually("the first generation activated and serving", func() bool {
		return len(s.activations(agentGRN)) == 1 && h.record().Memory.Served
	})
	oldToken := p.lastToken()
	require.NotEmpty(t, oldToken)
	before := s.generationOf(h, h.container("runtime"))

	codeword := "amber-" + strconv.Itoa(int(randomBytes(1)[0])+100)
	fact := "The codeword is " + codeword +
		". Keep it in mind. Reply to me with message_send, with the single word NOTED."
	from.post(agentGRN, "garam-sa-fact", fact)
	s.awaitCompleted(from.grn, "garam-sa-fact")
	from.observeReply(agentGRN, time.Minute)

	post := func(token, key string) (int, string) {
		status, raw := h.call(h.agentClient(), "/messages", "", "Garam-Execution "+token,
			map[string]string{"recipient": from.grn, "idempotencyKey": key, "body": key})
		return status, string(raw)
	}
	status, body := post(oldToken, "garam-sa-old-before")
	require.Equal(t, http.StatusCreated, status, "the control: the old token is current before the switch: %s", body)
	from.awaitBody(agentGRN, "garam-sa-old-before")
	require.NoError(t, first.stop(), s.scrub(first.out.String()))

	// The move.
	target := s.prefix + "agent-moved"
	start("--move-memory-to", target)
	s.eventually("the generation on the new volume activated", func() bool {
		a := s.activations(agentGRN)
		return len(a) == 2 && a[1].Ended == ""
	})
	activations := s.activations(agentGRN)
	after := s.generationOf(h, h.container("runtime"))
	assert.Equal(t, agentGRN, after.agent, "the same agent")
	assert.Equal(t, target, after.volume, "on the new volume")
	assert.NotEqual(t, before.nonce, after.nonce, "which the move created")
	assert.NotEqual(t, before.generation, after.generation, "a new generation")
	assert.Equal(t, activations[0].ID, h.record().Intent.Replaces, "its activation replaces the old one")
	assert.Equal(t, "superseded", activations[0].Ended)
	s.mustDocker("volume", "inspect", before.volume)
	// The store came across, read from the new volume's memory file:
	// what the old generation stored was checkpointed into it when its
	// runtime closed the store at its drain.
	factID := s.messages("sender_grn = $1 AND idempotency_key = $2", from.grn, "garam-sa-fact")[0].ID
	moved := s.sources(target, factID)
	require.Len(t, moved, 1, "the new volume's store holds the fact as one source")
	assert.Contains(t, moved[0], codeword)

	// The old generation's token is refused from the switch on, and
	// cannot claim; the new one is current.
	status, body = post(oldToken, "garam-sa-old-after")
	assert.Equal(t, http.StatusForbidden, status, "garam refuses the old token: %s", body)
	assert.JSONEq(t, `{"kind":"permission_denied","message":"execution token is not current"}`, body)
	claim := func(token string) (int, refusal) {
		status, raw := h.call(h.agentClient(), "/agents/"+agentGRN+"/claims", "", "Garam-Execution "+token, nil)
		var r refusal
		_ = json.Unmarshal(raw, &r)
		return status, r
	}
	status, refused := claim(oldToken)
	assert.Equal(t, http.StatusForbidden, status, "the old token claims nothing")
	assert.Equal(t, refusal{Kind: "permission_denied", Message: "execution token is not current"}, refused,
		"garam refuses it as a token that is not current")
	newToken := p.lastToken()
	require.NotEqual(t, oldToken, newToken)
	status, _ = claim(newToken)
	assert.Equal(t, http.StatusNoContent, status,
		"the control: the new token's claim is admitted, and finds nothing waiting")
	status, body = post(newToken, "garam-sa-new-after")
	require.Equal(t, http.StatusCreated, status, "the control: the new token is current: %s", body)
	from.awaitBody(agentGRN, "garam-sa-new-after")

	// A message after the switch reaches the new generation alone, and
	// the fact told before the move is recalled.
	question := "What is the codeword from my earlier message? Reply to me with message_send, with just the codeword."
	require.NotContains(t, question, codeword)
	// The test keeps a copy of every outbox entry the new runtime
	// writes, since the adapter removes each once garam stored it.
	runtime := h.container("runtime")
	s.placeProbe(runtime)
	s.mustDocker("exec", "-d", runtime, "/probe", "collect", "/data/outbox", "/data/garam-1180-collected")
	from.post(agentGRN, "garam-sa-recall", question)
	asked := []message{s.awaitCompleted(from.grn, "garam-sa-recall")}
	from.observeReply(agentGRN, time.Minute)

	// Only the new generation can have received it. Garam delivers at
	// least once (ADR-0018), so a lease that lapses during a slow turn
	// hands the question out again; what is asserted is which
	// generation can hold it, not how often it was held.
	var createdAt, claimedAt, activatedAt time.Time
	require.NoError(t, s.db().QueryRow(context.Background(),
		`SELECT m.created_at, m.claimed_at, a.created_at FROM messages m, agent_activations a WHERE m.id = $1 AND a.id::text = $2`,
		asked[0].ID, activations[1].ID).Scan(&createdAt, &claimedAt, &activatedAt))
	t.Logf("the question was claimed %d time(s)", asked[0].Attempts)
	assert.True(t, createdAt.After(activatedAt),
		"the question was sent after the new activation, so every claim of it came after the switch: sent %s, activated %s",
		createdAt, activatedAt)
	// Garam keeps the latest claim alone, so that is the one read; with
	// the claim above and the old token's refusal, no claim is left to
	// the old generation.
	assert.True(t, claimedAt.After(activatedAt),
		"and the claim garam holds was made under the new activation: claimed %s, activated %s",
		claimedAt, activatedAt)
	// Each answer the turn sent reaches garam once, however many times
	// the question was delivered: garam holds as many messages from the
	// agent to its sender since the question as the turns wrote outbox
	// entries in reply to it, under those entries' keys. One row per key
	// is the schema's; a second post under a fresh key is what this
	// would catch.
	var sent []outboxEntry
	for _, e := range s.collected(runtime) {
		if slices.Contains(e.InReplyTo, asked[0].ID) {
			sent = append(sent, e)
		}
	}
	t.Logf("the recall turn sent %d answer(s)", len(sent))
	for _, e := range sent {
		s.eventually("garam storing the answer "+e.ID, func() bool {
			return len(s.messages("sender_grn = $1 AND idempotency_key = $2", agentGRN, e.ID)) > 0
		})
	}
	var keys []string
	for _, m := range s.messages("sender_grn = $1 AND recipient_grn = $2 AND created_at > $3",
		agentGRN, from.grn, createdAt) {
		keys = append(keys, m.Key)
	}
	var written []string
	for _, e := range sent {
		written = append(written, e.ID)
	}
	assert.ElementsMatch(t, written, keys,
		"one durable reply per answer sent, and none other, since the question")

	// Sherlock's own stores, read once the new generation has stopped:
	// the question asked after the switch is in the new store alone.
	s.mustDocker("kill", "--signal", "TERM", h.container("runtime"))
	s.eventually("the new generation stopped", func() bool { return h.container("runtime") == "" })
	assert.Len(t, s.sources(target, asked[0].ID), 1, "the question reached the new generation's store")
	answered := s.scrub(strings.Join(s.answers(target, asked[0].ID), "\n"))
	t.Logf("the recall, as the turn recorded it: %q", answered)
	assert.Contains(t, strings.ToLower(answered), codeword, "memory from before the move is recalled")
	assert.Len(t, s.sources(before.volume, factID), 1, "the control: the old store holds the fact too")
	assert.Empty(t, s.sources(before.volume, asked[0].ID), "and not the question asked after the switch")
}

// refusal is garam's machine Error.
type refusal struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// keyName answers the variable the owner-only model environment file
// names, which the runtime's configuration reads its key from.
func keyName(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, line := range strings.Split(string(raw), "\n") {
		if name, _, ok := strings.Cut(strings.TrimSpace(line), "="); ok && name != "" && !strings.HasPrefix(name, "#") {
			return name
		}
	}
	t.Fatal("the model environment file names no variable")
	return ""
}

// awaitBody claims the sender's messages until one from the agent
// carries body, which the test posted with the agent's own token.
func (p *sender) awaitBody(from, body string) {
	p.s.t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if r := p.receive(); r != nil && r.Sender == from && r.Body == body {
			return
		}
	}
	p.s.t.Fatalf("no message %q from %s", body, from)
}
