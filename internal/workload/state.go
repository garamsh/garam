package workload

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/keyfile"
)

// stateFile is the supervisor's record of what it owns, in its own
// directory. Nothing else writes it, and it is the only evidence the
// supervisor adopts a container or a volume on.
const stateFile = "workload.json"

// Labels the supervisor writes on what it creates. They confirm a
// record and never stand in for one: anyone who can reach the engine
// can write a label.
const (
	labelNonce  = "sh.garam.workload.nonce"
	labelIntent = "sh.garam.workload.intent"
)

// state is the supervisor's durable record.
type state struct {
	Memory    *volumeRecord `json:"memory,omitempty"`
	Workspace *volumeRecord `json:"workspace,omitempty"`
	// LastActivation is the agent's most recent activation, which the
	// next one names as the one it replaces.
	LastActivation string `json:"lastActivation,omitempty"`
	// Generations is every generation an activation was ever asked
	// for, which none is accepted for again.
	Generations []string `json:"generations,omitempty"`
	Intent      *intent  `json:"intent,omitempty"`
	// Move is the latest move of the memory to another volume.
	Move *move `json:"move,omitempty"`
}

// volumeRecord is a volume the supervisor created, recognised by the
// nonce it labelled it with. The record is written before the volume is
// created, so a crash between the two leaves a record whose volume is
// either absent or carries the nonce.
type volumeRecord struct {
	Name  string `json:"name"`
	Nonce string `json:"nonce"`
	// Confirmed is the volume having been seen carrying the nonce:
	// from then on an absent volume is a lost one, never one to create.
	Confirmed bool `json:"confirmed"`
	// Served is a runtime having reported serving on this memory
	// volume, so its store exists and every later start requires it.
	Served bool `json:"served,omitempty"`
	// WriterExit is the status the last runtime that ran on this memory
	// volume exited with, as its drain observed it, and absent where no
	// drain did. A move takes only a store whose writer exited 0.
	WriterExit *int `json:"writerExit,omitempty"`
}

// phase is where an intent stands.
type phase string

const (
	// phaseStarting is an intent being brought up or running.
	phaseStarting phase = "starting"
	// phaseDraining is an intent whose runtime and workspace were told
	// to stop, which ends once both have exited.
	phaseDraining phase = "draining"
	// phaseEnded is an intent whose containers have exited and are
	// removed.
	phaseEnded phase = "ended"
)

// intent is one generation the supervisor means to run: written before
// anything of it is created, so a crash at any point leaves a record of
// what was asked for and under which names.
type intent struct {
	ID       string `json:"id"`
	Nonce    string `json:"nonce"`
	Phase    phase  `json:"phase"`
	Epoch    string `json:"epoch"`
	Revision string `json:"revision"`
	Images   Images `json:"images"`
	// RequireStore is whether the runtime starts with --require-store:
	// set from the memory volume's Served when the intent is written.
	RequireStore bool `json:"requireStore"`
	// Attached is an intent for a runtime the host runs itself, which
	// the supervisor attached to and created no container of
	// (ADR-0111).
	Attached bool `json:"attached,omitempty"`
	// PlacementToken is what the adapter presents on activation.
	PlacementToken string `json:"placementToken"`
	// ControlToken is what the generation's adapter presents to this
	// supervisor's control, and empty in an intent a certificate-mode run
	// recorded before ADR-0118. The supervisor draws it with the intent
	// and never sends it to garam, so garam accepts it nowhere and control
	// accepts nothing else (ADR-0100).
	ControlToken string `json:"controlToken,omitempty"`

	Runtime   containerRecord `json:"runtime"`
	Workspace containerRecord `json:"workspace"`
	Adapter   containerRecord `json:"adapter"`

	// Generation is the runtime's start nonce the first activation was
	// asked for, RequestID the identifier it was asked under and
	// Replaces the activation it named; a retry repeats all three.
	Generation   string `json:"generation,omitempty"`
	RequestID    string `json:"requestId,omitempty"`
	Replaces     string `json:"replaces,omitempty"`
	ActivationID string `json:"activationId,omitempty"`
}

// containerRecord is one container of an intent: its name, derived
// before it exists, the ID the engine answered and when it started.
type containerRecord struct {
	Name      string    `json:"name"`
	ID        string    `json:"id,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`
}

func (in *intent) container(r Role) *containerRecord {
	switch r {
	case RoleRuntime:
		return &in.Runtime
	case RoleWorkspace:
		return &in.Workspace
	case RoleAdapter:
		return &in.Adapter
	}
	panic("workload: unknown role " + string(r))
}

// live reports whether the intent may still run.
func (in *intent) live() bool {
	return in != nil && in.Phase == phaseStarting
}

// seen reports whether a generation was ever asked to be activated.
func (st *state) seen(generation string) bool {
	for _, g := range st.Generations {
		if g == generation {
			return true
		}
	}
	return false
}

// loadState reads the record, or answers an empty one where none was
// written yet.
func loadState(dir string) (*state, error) {
	path := filepath.Join(dir, stateFile)
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return &state{}, nil
	}
	raw, err := keyfile.Read(path)
	if err != nil {
		return nil, errorx.Wrap(err, "workload: record")
	}
	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, errorx.Wrap(err, "workload: record", errorx.KindDataLoss)
	}
	return &st, nil
}

// save replaces the record in one rename, after the data and then the
// directory entry reach the disk, so a crash leaves the old record or
// the new one and never part of either.
func (st *state) save(dir string) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return errorx.Wrap(err, "workload: record", errorx.KindInternal)
	}
	return writeDurably(filepath.Join(dir, stateFile), raw, 0o600)
}

func writeDurably(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return errorx.Wrap(err, "workload: write "+path, errorx.KindUnavailable)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	err = tmp.Chmod(perm)
	if err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err == nil {
		err = syncDir(dir)
	}
	if err != nil {
		return errorx.Wrap(err, "workload: write "+path, errorx.KindUnavailable)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// randomHex answers n random bytes as lowercase hex.
func randomHex(n int) string {
	b := make([]byte, n)
	// crypto/rand.Read never returns an error; it crashes the program
	// when the system's randomness source fails.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
