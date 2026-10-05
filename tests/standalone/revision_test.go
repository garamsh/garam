package standalone_test

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStandalone_ChangesItsConfigurationToANewReportedRevision is
// #1157 scenario 4 on a standalone host (#1203), through the shipped
// `garam standalone run`, on a real garam and a real model: a
// configuration changed from revision R1 to R2 and run with
// `--replace` is a new generation of the same agent on the same memory
// volume, and its runtime reports R2 as the revision it applied, as do
// the supervisor's record and the runtime status the adapter forwards.
// The fact told under R1 is still in the runtime's store. The control
// is the same configuration run again, which keeps R1 and its
// generation; a configuration with no revision, or one that does not
// parse, is refused before anything is drained.
func TestStandalone_ChangesItsConfigurationToANewReportedRevision(t *testing.T) {
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

	config := func(name, revision string) string {
		line := ""
		if revision != "" {
			line = "revision: " + revision + "\n"
		}
		return s.file(name, line+fmt.Sprintf(`model:
  provider: openai-compatible
  base-url: https://api.minimax.io/v1
  model: MiniMax-M2
  api-key-env: %s
embedding:
  base-url: http://%s:80/v1
  model: BAAI/bge-base-en-v1.5
`, keyName(t, modelEnv), tei))
	}
	r1, r2 := config("config-r1.yaml", "r1-config"), config("config-r2.yaml", "r2-config")
	extra := []string{"--instructions-file", s.file("instructions.md", replyInstruction)}
	var runs []*running
	start := func(cfg string, more ...string) *running {
		r := h.run(runOptions{config: cfg, modelEnv: modelEnv, extra: append(append([]string{}, extra...), more...)})
		runs = append(runs, r)
		return r
	}
	t.Cleanup(func() {
		if t.Failed() {
			for i, r := range runs {
				t.Logf("supervisor run %d:\n%s", i, s.scrub(tail(r.out.String(), 40)))
			}
		}
	})
	// reported answers what the runtime's own status says it applied,
	// read on its loopback from inside the adapter.
	reported := func() string {
		said, err := s.probeIn(h.container("adapter"), "get", "http://127.0.0.1:8080/agents/"+agentGRN+"/status")
		if err != nil {
			return ""
		}
		var status struct {
			Revision string `json:"config_revision"`
		}
		_ = json.Unmarshal([]byte(said), &status)
		return status.Revision
	}

	first := start(r1)
	s.eventually("the first generation activated and serving", func() bool {
		return len(s.activations(agentGRN)) == 1 && h.record().Memory.Served
	})
	assert.Equal(t, "r1-config", reported(), "the runtime reports R1")
	before := s.generationOf(h, h.container("runtime"))
	codeword := "cedar-" + strconv.Itoa(int(randomBytes(1)[0])+100)
	from.post(agentGRN, "garam-sa-fact", "The codeword is "+codeword+
		". Keep it in mind. Reply to me with message_send, with the single word NOTED.")
	t.Logf("the fact's reply: %q", s.scrub(from.awaitReply(agentGRN).Body))
	factID := s.messages("sender_grn = $1 AND idempotency_key = $2", from.grn, "garam-sa-fact")[0].ID
	require.NoError(t, first.stop(), s.scrub(first.out.String()))

	// The control: the same configuration again adopts the generation
	// running R1.
	runtime := h.container("runtime")
	same := start(r1)
	s.eventually("the supervisor serving the adopted generation", func() bool {
		sockets, _ := filepath.Glob(filepath.Join(h.supervisor, "generations", "*", "control", "control.sock"))
		if len(sockets) != 1 {
			return false
		}
		conn, err := net.Dial("unix", sockets[0])
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	})
	assert.Equal(t, runtime, h.container("runtime"), "the control: an unchanged configuration keeps its runtime")
	assert.Len(t, s.activations(agentGRN), 1, "and its activation")
	assert.Equal(t, "r1-config", reported(), "and R1")
	assert.Equal(t, "r1-config", h.record().Intent.Revision)
	require.NoError(t, same.stop(), s.scrub(same.out.String()))

	// The refusals: neither configuration drains anything.
	for name, bad := range map[string]string{
		"no revision": config("config-none.yaml", ""),
		"no parse":    s.file("config-broken.yaml", "revision: r2-config\nmodel: [unclosed\n"),
	} {
		r := start(bad, "--replace")
		select {
		case <-r.done:
		case <-time.After(2 * time.Minute):
			t.Fatalf("%s: the run did not return", name)
		}
		assert.Error(t, r.err, "%s is refused", name)
		assert.Contains(t, r.out.String(), "runtime configuration", name)
		assert.Equal(t, runtime, h.container("runtime"), "%s: the old runtime runs on", name)
		assert.Len(t, s.activations(agentGRN), 1, "%s: nothing is activated", name)
	}

	// The change.
	second := start(r2, "--replace")
	s.eventually("a new runtime", func() bool {
		id := h.container("runtime")
		return id != "" && id != runtime
	})
	s.eventually("the runtime reporting R2", func() bool { return reported() == "r2-config" })
	assert.Equal(t, "r2-config", h.record().Intent.Revision, "the supervisor's record names R2")
	s.eventually("the generation running R2 activated", func() bool {
		a := s.activations(agentGRN)
		return len(a) == 2 && a[1].Ended == ""
	})
	after := s.generationOf(h, h.container("runtime"))
	assert.Equal(t, before.agent, after.agent, "the same agent")
	assert.Equal(t, before.volume, after.volume, "on the same memory volume")
	assert.Equal(t, before.nonce, after.nonce)
	assert.NotEqual(t, before.generation, after.generation, "a new generation")
	state := func(id string) string {
		said, _ := s.probeIn(h.container("adapter"), "get", "http://127.0.0.1:8080/agents/"+agentGRN+"/messages/"+id)
		var m struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal([]byte(said), &m)
		return m.Status
	}
	assert.Equal(t, "completed", state(factID), "the runtime on R2 knows the fact it completed under R1")
	assert.Equal(t, "unknown", state("garam-sa-never-sent"), "the control: an id it never had")

	// The forwarded report, as the supervisor logged accepting it, and
	// the store, once the generation has stopped.
	s.mustDocker("kill", "--signal", "TERM", h.container("runtime"))
	<-second.done
	said := second.out.String()
	assert.Regexp(t, `INFO runtime status .*revision=r2-config state=serving`, said,
		"the adapter forwarded the runtime's R2 report, which the supervisor accepted")
	assert.NotContains(t, said, "revision=r1-config", "and no R1 report for this generation")
	sources := s.sources(after.volume, factID)
	require.Len(t, sources, 1, "the store holds the fact as one source")
	assert.Contains(t, sources[0], codeword)
}
