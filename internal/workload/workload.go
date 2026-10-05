// Package workload runs a standalone host's agent as the workload its
// supervisor owns (ADR-0082, ADR-0085): one generation at a time, as a
// runtime container that owns a network namespace, a workspace
// container and the garam adapter joined to it, over a memory volume
// and a workspace volume the supervisor created and recognises by a
// nonce. It serves the adapter's activation and runtime-status contract
// (ADR-0084) on a socket only that generation's adapter mounts, and
// forwards an activation to garam only for the generation it owns.
//
// The engine is reached through Engine; garam through Activator. Every
// decision is taken from a durable record the supervisor alone writes,
// never from what an engine label says.
package workload

import (
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Refusals the supervisor leaves pending rather than repairs. Each
// needs a person: the supervisor never removes, adopts or replaces what
// it cannot prove it owns.
var (
	// ErrStorageMissing is a volume the record says was created and the
	// engine no longer holds.
	ErrStorageMissing = errorx.New(errorx.KindFailedPrecondition,
		"workload: a volume this host created is gone; continuing needs an explicit handover")
	// ErrStorageReplaced is a volume of the recorded name that does not
	// carry the recorded nonce: recreated, or never this host's.
	ErrStorageReplaced = errorx.New(errorx.KindFailedPrecondition,
		"workload: a volume does not carry the nonce this host created it with; continuing needs an explicit handover")
	// ErrForeign is a container under a name this host derived that it
	// cannot prove it created.
	ErrForeign = errorx.New(errorx.KindFailedPrecondition,
		"workload: a container under this host's name is not the one it created; it is left untouched")
	// ErrOutboxReplaced is the memory volume's outbox being something
	// other than a directory, which the adapter's mount of it alone
	// would not confine.
	ErrOutboxReplaced = errorx.New(errorx.KindFailedPrecondition,
		"workload: the memory volume's outbox is not a directory; continuing needs an explicit handover")
	// ErrGenerationEnded is the generation the supervisor ran having
	// exited, after the rest of it was drained.
	ErrGenerationEnded = errorx.New(errorx.KindAborted, "workload: the generation exited")
)

// Engine sentinels, which an Engine answers in place of its own.
var (
	// ErrAbsent is a container or volume the engine does not hold.
	ErrAbsent = errorx.New(errorx.KindNotFound, "workload: not held by the engine")
	// ErrNameInUse is a create refused because the name is taken.
	ErrNameInUse = errorx.New(errorx.KindAlreadyExists, "workload: the name is in use")
)

// Images are the three images a generation runs, each named by digest.
type Images struct {
	Runtime   string `json:"runtime"`
	Workspace string `json:"workspace"`
	Adapter   string `json:"adapter"`
}

// Role is one of the three containers of a generation.
type Role string

const (
	// RoleRuntime is the agent's runtime: the memory writer, owner of
	// the network namespace the other two join.
	RoleRuntime Role = "runtime"
	// RoleWorkspace is the runtime's exec and file plane.
	RoleWorkspace Role = "workspace"
	// RoleAdapter is garam's adapter for the agent.
	RoleAdapter Role = "adapter"
)

// roles is the order a generation is brought up in. The runtime comes
// first because the other two join its network namespace.
var roles = []Role{RoleRuntime, RoleWorkspace, RoleAdapter}

// Desired is what the host asks the supervisor to run.
type Desired struct {
	// Agent is the agent this host was enrolled for, and Epoch its
	// assignment's epoch, as the enrollment recorded them.
	Agent grn.GRN
	Epoch string
	// Config is the runtime's configuration file and Revision the
	// revision it names, which the runtime echoes in its status.
	Config   []byte
	Revision string
	// Ego and Instructions are the runtime's ego and operator
	// instructions, each absent where nil: no ego is the runtime's
	// own default. The revision names them as it names the
	// configuration, so changing either is a new revision.
	Ego          []byte
	Instructions []byte
	// ModelEnv is the runtime's environment: the model credential and
	// nothing garam holds.
	ModelEnv []string
	Images   Images
	// Replace starts a new generation even where the current one runs
	// what is desired.
	Replace bool
}

// AdapterFiles is what the adapter presents and verifies by, copied
// into its container before it starts.
type AdapterFiles struct {
	CertificatePEM []byte
	KeyPEM         []byte
	IssuerPEM      []byte
	ServerRootPEM  []byte
	MachineURL     string
}
