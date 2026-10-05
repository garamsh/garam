package workload_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/workload"
)

// TestRealEngine_TheAdapterMountsTheOutboxAlone holds the adapter to the
// memory volume's outbox (ADR-0085): it cannot open the store by any
// path, and the outbox is all of the volume it mounts. The control is
// the adapter forwarding an entry the runtime's account wrote there and
// removing it once garam stored it.
func TestRealEngine_TheAdapterMountsTheOutboxAlone(t *testing.T) {
	h := newHost(t)
	stand := h.garamOn()
	h.start()
	generation := h.activated(1)
	runtime, adapter := h.container(workload.RoleRuntime), h.container(workload.RoleAdapter)
	require.Equal(t, "ok", h.report(runtime, "--open", "/data/memory.db").Open["/data/memory.db"],
		"the store exists on the volume")

	ad := h.report(adapter, "--open", "/data/memory.db", "--open", "/outbox/memory.db",
		"--open", "/outbox/../memory.db", "--open", "/memory.db")
	for path, got := range ad.Open {
		assert.NotEqual(t, "ok", got, "the adapter opens %s", path)
	}
	assert.Contains(t, ad.Mounts, "/outbox")
	assert.NotContains(t, ad.Mounts, "/data")

	id := "garam-1180-" + strings.TrimSuffix(strings.TrimPrefix(h.prefix, testPrefix), "-")
	entry, err := json.Marshal(map[string]any{
		"id": id, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "agent_id": testAgent.String(),
		"assignment_epoch": "3", "generation": generation, "in_reply_to": []string{}, "channel": "garam",
		"target": "grn:acme:default:agent:ffffffffffffffff", "content": "hello",
	})
	require.NoError(t, err)
	h.mustDocker("exec", runtime, h.probe(runtime), "put", "/data/outbox", id+".json", string(entry))
	eventually(t, "the entry stored", func() bool {
		return strings.Contains(h.mustDocker("logs", stand), "stored "+id)
	})
	eventually(t, "the entry removed", func() bool { return h.size(runtime, "/data/outbox/"+id+".json") == -1 })
}

// TestRealEngine_RefusesAnOutboxThatIsNotADirectory holds the guard
// before the adapter's mount: a link inside the volume, which the
// engine's subpath would follow to the whole volume, and a regular
// file where the outbox was each refuse, create no adapter, and leave
// the path as it was. The control is the outbox restored as a
// directory, on which the next generation runs.
func TestRealEngine_RefusesAnOutboxThatIsNotADirectory(t *testing.T) {
	h := newHost(t)
	r := h.start()
	h.activated(1)
	h.mustDocker("kill", "--signal", "TERM", h.container(workload.RoleRuntime))
	require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
	memory := h.volume("memory")
	plant := func(script string) {
		t.Helper()
		h.mustDocker("run", "--rm", "--user", "65532:65532", "--network", "none",
			"--mount", "type=volume,src="+memory+",dst=/v", "--entrypoint", "sh", h.images.Workspace, "-c", script)
	}
	look := func() string {
		t.Helper()
		return h.mustDocker("run", "--rm", "--network", "none", "--mount", "type=volume,src="+memory+",dst=/v",
			"--entrypoint", "stat", h.images.Workspace, "-c", "%F", "/v/outbox")
	}

	plant("mv /v/outbox /v/outbox.kept && ln -s . /v/outbox")
	require.ErrorIs(t, h.start().result(t), workload.ErrOutboxReplaced, "a link inside the volume")
	assert.Empty(t, h.container(workload.RoleAdapter), "no adapter is created")
	assert.Equal(t, "symbolic link", look(), "the path is left as it was")

	plant("rm /v/outbox && touch /v/outbox")
	require.ErrorIs(t, h.start().result(t), workload.ErrOutboxReplaced, "a regular file")
	assert.Empty(t, h.container(workload.RoleAdapter), "no adapter is created")
	assert.Equal(t, "regular empty file", look(), "the path is left as it was")

	plant("rm /v/outbox && mv /v/outbox.kept /v/outbox")
	h.start()
	h.activated(2)
}
