// Package guest is the supervisor that runs inside a gaon MicroVM: it
// starts the agent runtime, the workspace and garam's adapter in that
// order as one user and drains them in the same order, so the adapter,
// which carries the runtime's messages, outlives it (ADR-0151 §3).
package guest

import (
	"io"
	"log/slog"
)

// What the operator renders and the supervisor reads. The operator's
// MicroVM names these paths and this port, so both sides share them.
const (
	// UID is the user the supervisor and its three programs run as.
	UID = 65532
	// CredentialsDir holds every Secret-sourced file, mode 0400.
	CredentialsDir = "/run/garam/"
	// MemoryDir and WorkspaceDir are where the two Volumes mount.
	MemoryDir    = "/var/lib/garam/memory"
	WorkspaceDir = "/workspace"
	// ReadyPort is the supervisor's readiness listener.
	ReadyPort = 8088
)

// The files below CredentialsDir, named as the operator's Secret keys.
// ControlTokenFile is named by the adapter's contract and rendered by
// no operator yet.
const (
	GaramRootFile      = "garam-root.pem"
	ControlRootFile    = "control-root.pem"
	AgentTokenFile     = "agent-token"
	PlacementTokenFile = "placement-token"
	ControlTokenFile   = "control-token"
)

// Program is one process of a generation. Its environment is exactly
// Env: nothing is inherited from the supervisor.
type Program struct {
	Name string
	Path string
	Args []string
	Env  []string
}

// Config is one generation: the programs in start order, where the
// readiness listener opens once all of them run, and where their output
// and the supervisor's log go.
type Config struct {
	Programs  []Program
	ReadyAddr string
	Stdout    io.Writer
	Stderr    io.Writer
	Log       *slog.Logger
}
