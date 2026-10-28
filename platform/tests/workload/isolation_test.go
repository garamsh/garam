package workload_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/workload"
)

// TestRealEngine_ActivatesItsGenerationAndIsolatesIt drives the shipped
// arrangement on a real daemon (T12, and the workspace probe of
// #1180's decision). Pinned Sherlock reports its status on loopback,
// the garam adapter beside it reads it and asks the supervisor over its
// generation's socket, and the supervisor forwards the activation with
// the leaf the adapter presented. Then a process in each container
// reports what it can reach. The runtime, where tools run, holds no
// capability, no garam material, no engine socket and no control
// socket; the workspace, where Exec children run, has no memory volume
// at all. The controls are in the same reports: the runtime writes its
// memory directory and outbox, the workspace writes its own volume,
// and the adapter reaches its socket and the outbox.
func TestRealEngine_ActivatesItsGenerationAndIsolatesIt(t *testing.T) {
	h := newHost(t)
	r := h.start()
	generation := h.activated(1)
	assert.Regexp(t, "^[0-9a-f]{32}$", generation)
	asked := h.garam.requests[0]
	assert.Equal(t, "3", asked.Epoch)
	assert.Empty(t, asked.Replaces, "the first activation names no anchor")
	assert.Equal(t, h.token, asked.Token, "the agent's token the supervisor holds is what garam is shown")

	runtime, space, adapter := h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace), h.container(workload.RoleAdapter)
	reachable := []string{
		"--open", "/run/garam-control/control.sock",
		"--open", "/etc/garam/agent-token",
		"--open", "/etc/garam/placement-token",
		"--open", "/var/run/docker.sock",
		"--open", "/run/docker.sock",
		"--open", h.dir + "/workload.json",
	}

	rt := h.report(runtime, append(reachable, "--write", "/data", "--write", "/data/outbox")...)
	assert.Equal(t, "65532 65532 65532 65532", rt.Status["Uid"])
	assert.Subset(t, []string{"65532"}, strings.Fields(rt.Status["Groups"]), "no group beyond the runtime's own")
	assert.Equal(t, "0000000000000000", rt.Status["CapEff"], "a tool holds no capability")
	assert.Equal(t, "1", rt.Status["NoNewPrivs"])
	for _, key := range rt.Env {
		assert.False(t, strings.HasPrefix(key, "GARAM_"), "the runtime's environment carries garam's %s", key)
		assert.False(t, strings.HasPrefix(key, "DOCKER_"), "the runtime's environment carries %s", key)
	}
	assert.Contains(t, rt.Env, "MODEL_KEY", "the control: the model credential is the runtime's")
	assert.NotContains(t, rt.Mounts, "/run/garam-control")
	assert.NotContains(t, rt.Mounts, "/var/lib/sherlock-workspace")
	for path, got := range rt.Open {
		assert.NotEqual(t, "ok", got, "the runtime reaches %s", path)
	}
	assert.Equal(t, map[string]string{"/data": "ok", "/data/outbox": "ok"}, rt.Write,
		"the control: the runtime writes and reads its memory directory and outbox")

	ws := h.report(space, append(reachable,
		"--open", "/data/memory.db", "--open", "/data/outbox",
		"--write", "/var/lib/sherlock-workspace")...)
	assert.NotContains(t, ws.Mounts, "/data", "the memory volume is not mounted where Exec children run")
	for path, got := range ws.Open {
		assert.NotEqual(t, "ok", got, "the workspace reaches %s", path)
	}
	assert.Equal(t, map[string]string{"/var/lib/sherlock-workspace": "ok"}, ws.Write,
		"the control: the workspace writes its own volume")

	ad := h.report(adapter, "--open", "/run/garam-control/control.sock", "--open", "/etc/garam/agent-token",
		"--open", "/var/run/docker.sock", "--open", "/run/docker.sock", "--write", "/outbox")
	assert.Equal(t, "0000000000000000", ad.Status["CapEff"])
	assert.Equal(t, "ok", ad.Open["/run/garam-control/control.sock"], "the control: the adapter reaches its socket")
	assert.Equal(t, "ok", ad.Open["/etc/garam/agent-token"], "and its own token")
	assert.NotEqual(t, "ok", ad.Open["/var/run/docker.sock"])
	assert.NotEqual(t, "ok", ad.Open["/run/docker.sock"])
	assert.Equal(t, "ok", ad.Write["/outbox"], "and the outbox")
	assert.NotContains(t, ad.Mounts, "/var/lib/sherlock-workspace")

	require.NoError(t, r.stop(), "stopping the supervisor leaves the workload")
	assert.Equal(t, "true", h.inspect(runtime, "{{.State.Running}}"), "and the runtime runs on")
}
