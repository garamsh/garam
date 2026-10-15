package workload

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// Where the copy's helper mounts the two volumes: the source
// read-only, the target as it will be written.
const (
	sourceTarget = "/source"
	copyTarget   = "/target"
)

// CopyCommand is what the copy's helper runs in the adapter's image:
// the garam command that calls CopyStore.
const CopyCommand = "copy-memory"

// moveOutcome is how a move settled; empty while it is not settled.
type moveOutcome string

const (
	// moveAccepted is a move whose runtime on the new volume served
	// under --require-store and was asked to be activated.
	moveAccepted moveOutcome = "accepted"
	// moveRefused is a move that left the agent on the volume it
	// moved from.
	moveRefused moveOutcome = "refused"
)

// move is one move of the agent's memory to a volume of its own. It is
// recorded once the old generation has exited and before anything of
// the move exists.
type move struct {
	ID    string       `json:"id"`
	Nonce string       `json:"nonce"`
	From  volumeRecord `json:"from"`
	To    volumeRecord `json:"to"`
	// Image is what the copy's helper runs, and Copier the helper.
	Image  string          `json:"image"`
	Copier containerRecord `json:"copier"`
	// Source is the digest of the source's state before the copy.
	Source string `json:"source,omitempty"`
	// Digest is the verified copy's. Once it is set the record's
	// memory is To, until the move settles.
	Digest string `json:"digest,omitempty"`
	// Intent is the generation started on To, whose first activation
	// request accepts the move.
	Intent  string      `json:"intent,omitempty"`
	Outcome moveOutcome `json:"outcome,omitempty"`
	Reason  string      `json:"reason,omitempty"`
}

func (mv *move) settled() bool { return mv != nil && mv.Outcome != "" }

// awaiting reports whether a verified copy waits for its runtime's
// acceptance.
func (mv *move) awaiting() bool { return mv != nil && mv.Outcome == "" && mv.Digest != "" }

// moveOwed answers whether this run owes a copy before its next
// generation: a move asked for and not yet verified. A move recorded
// and not settled holds every run to it, and one refused is not tried
// again.
func (s *Supervisor) moveOwed() (bool, error) {
	to, mv := s.cfg.Desired.MoveMemoryTo, s.st.Move
	switch {
	case mv != nil && !mv.settled() && mv.To.Name != to:
		return false, errorx.Wrap(ErrMovePending,
			"the memory's move to "+mv.To.Name+" is not settled; run again with --move-memory-to "+mv.To.Name)
	case mv != nil && !mv.settled():
		return mv.Digest == "", nil
	case to == "" || to == s.st.Memory.Name:
		return false, nil
	case mv != nil && mv.To.Name == to:
		return false, errorx.Wrap(ErrMoveRefused, mv.Reason+"; the volume of a refused move is not used again")
	}
	return true, nil
}

// movable refuses a move from a store whose writer was not positively
// stopped: exited 0 after its drain, as Sherlock requires of a copy
// (deployment.md §Snapshot and restore at v0.8.0). The writer lock,
// the other half, is the copy's to hold.
func (s *Supervisor) movable() error {
	m := s.st.Memory
	switch {
	case !m.Served:
		return errorx.Wrap(ErrMoveRefused, "no runtime has served on "+m.Name+", so it holds no store to move")
	case m.WriterExit == nil:
		return errorx.Wrap(ErrMoveRefused, "how the last runtime on "+m.Name+" exited was not observed")
	case *m.WriterExit != 0:
		return errorx.Wrap(ErrMoveRefused, fmt.Sprintf("the last runtime on %s exited %d rather than after a drain", m.Name, *m.WriterExit))
	}
	return nil
}

// move copies the memory to the volume asked for and verifies the
// copy, with the old generation already exited. The copy is made by a
// helper holding the source's writer lock, with the source mounted
// read-only; it is verified by reading the source before and after
// and the target once, each through the engine, and comparing their
// digests. A verified copy becomes the record's memory, which the next
// generation starts on with --require-store. Anything else refuses the
// move and leaves the record's memory where it was.
func (s *Supervisor) move(ctx context.Context) error {
	mv := s.st.Move
	if mv == nil || mv.settled() {
		if err := s.movable(); err != nil {
			return err
		}
		id := randomHex(8)
		mv = &move{
			ID: id, Nonce: randomHex(16), From: *s.st.Memory,
			To:    volumeRecord{Name: s.cfg.Desired.MoveMemoryTo, Nonce: randomHex(16)},
			Image: s.cfg.Desired.Images.Adapter, Copier: containerRecord{Name: s.cfg.Prefix + "copier-" + id},
		}
		if err := s.locked(func() { s.st.Move = mv }); err != nil {
			return err
		}
		slog.Info("memory move recorded", slog.String("move", mv.ID), slog.String("from", mv.From.Name),
			slog.String("to", mv.To.Name))
	}
	if err := s.ensureVolume(ctx, &mv.To); err != nil {
		if errors.Is(err, ErrStorageMissing) || errors.Is(err, ErrStorageReplaced) {
			return s.refuseMove(ctx, mv, "the volume "+mv.To.Name+" is not one this move created")
		}
		return err
	}
	c, err := s.copier(ctx, mv)
	if err != nil {
		return err
	}
	if c.Status == ContainerCreated {
		root := File{Path: strings.TrimPrefix(copyTarget, "/"), Mode: 0o755, Dir: true, UID: runtimeUID, GID: runtimeUID}
		if err := s.engine.CopyFiles(ctx, c.ID, "/", []File{root}); err != nil {
			return err
		}
		before, _, err := s.stateDigest(ctx, c.ID, sourceTarget)
		if err != nil {
			return err
		}
		if err := s.locked(func() { mv.Source = before }); err != nil {
			return err
		}
		if err := s.engine.StartContainer(ctx, c.ID); err != nil {
			return err
		}
	}
	if err := s.engine.WaitExited(ctx, c.ID); err != nil {
		return err
	}
	if c, err = s.engine.InspectContainer(ctx, c.ID); err != nil {
		return err
	}
	if c.ExitCode != 0 {
		said, err := s.engine.Logs(ctx, c.ID)
		if err != nil {
			return err
		}
		return s.refuseMove(ctx, mv, fmt.Sprintf("the copy exited %d: %s", c.ExitCode, lastLine(said)))
	}
	copied, extra, err := s.stateDigest(ctx, c.ID, copyTarget)
	if err != nil {
		return err
	}
	after, _, err := s.stateDigest(ctx, c.ID, sourceTarget)
	if err != nil {
		return err
	}
	switch {
	case after != mv.Source:
		return s.refuseMove(ctx, mv, "the source changed while it was copied")
	case len(extra) > 0:
		return s.refuseMove(ctx, mv, "the copy holds "+strings.Join(extra, ", ")+", which the source's state does not")
	case copied != mv.Source:
		return s.refuseMove(ctx, mv, "the copy's digest does not match its source's")
	}
	if err := s.engine.RemoveContainer(ctx, c.ID); err != nil && !errors.Is(err, ErrAbsent) {
		return err
	}
	slog.Info("memory copy verified", slog.String("move", mv.ID), slog.String("digest", copied))
	return s.locked(func() {
		mv.Digest = copied
		s.st.Memory = &volumeRecord{Name: mv.To.Name, Nonce: mv.To.Nonce, Confirmed: true, Served: true}
	})
}

// copier answers the move's helper: found by its recorded ID, or by its
// name where its labels, image and volumes confirm the move, or
// created. One the record names that is gone leaves the copy unknown,
// which refuses the move.
func (s *Supervisor) copier(ctx context.Context, mv *move) (*ContainerState, error) {
	rec := &mv.Copier
	nameOrID := rec.Name
	if rec.ID != "" {
		nameOrID = rec.ID
	}
	c, err := s.engine.InspectContainer(ctx, nameOrID)
	switch {
	case errors.Is(err, ErrAbsent) && rec.ID != "":
		return nil, s.refuseMove(ctx, mv, "the copy's helper is gone, so what it copied is unknown")
	case errors.Is(err, ErrAbsent):
		id, err := s.engine.CreateContainer(ctx, ContainerSpec{
			Name: rec.Name, Image: mv.Image, Labels: map[string]string{labelNonce: mv.Nonce, labelIntent: mv.ID},
			User: runtimeUser, Cmd: []string{CopyCommand, "--from", sourceTarget, "--to", copyTarget}, NoNetwork: true,
			Volumes: []VolumeMount{
				{Volume: mv.From.Name, Target: sourceTarget, ReadOnly: true},
				{Volume: mv.To.Name, Target: copyTarget},
			},
		})
		if err != nil {
			return nil, err
		}
		if err := s.locked(func() { rec.ID = id }); err != nil {
			return nil, err
		}
		for _, v := range []*volumeRecord{&mv.From, &mv.To} {
			if err := s.verifyVolume(ctx, v); err != nil {
				return nil, err
			}
		}
		return s.engine.InspectContainer(ctx, id)
	case err != nil:
		return nil, err
	}
	if c.Labels[labelNonce] != mv.Nonce || c.Labels[labelIntent] != mv.ID || c.Image != mv.Image ||
		c.Volumes[sourceTarget] != mv.From.Name || c.Volumes[copyTarget] != mv.To.Name {
		return nil, errorx.Wrap(ErrForeign, nameOrID)
	}
	if rec.ID == "" {
		if err := s.locked(func() { rec.ID = c.ID }); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// refuseMove settles a move as refused, with its reason: the record's
// memory is the volume it moved from, so the agent stays stopped on
// it, and the volume moved to is kept and never used again.
func (s *Supervisor) refuseMove(ctx context.Context, mv *move, reason string) error {
	if err := s.locked(func() {
		mv.Outcome, mv.Reason = moveRefused, reason
		if s.st.Memory.Name == mv.To.Name {
			from := mv.From
			s.st.Memory = &from
		}
	}); err != nil {
		return err
	}
	if mv.Copier.ID != "" {
		if err := s.engine.RemoveContainer(ctx, mv.Copier.ID); err != nil && !errors.Is(err, ErrAbsent) {
			return err
		}
	}
	return errorx.Wrap(ErrMoveRefused, reason)
}

// moveEnded refuses a move whose generation on the new volume ended
// before an activation was asked for it, so its runtime's acceptance
// of the copy under --require-store was never seen.
func (s *Supervisor) moveEnded(ctx context.Context) error {
	mv := s.st.Move
	if !mv.awaiting() || mv.Intent == "" {
		return nil
	}
	if in := s.st.Intent; in != nil && in.ID == mv.Intent && in.Phase != phaseEnded {
		return nil
	}
	return s.refuseMove(ctx, mv, "the generation started on "+mv.To.Name+
		" ended before it was activated, so its runtime never served on the copy under --require-store")
}

// stateDigest reads path, a volume a container mounts, through the
// engine, and answers its manifest's digest and what else it holds.
func (s *Supervisor) stateDigest(ctx context.Context, id, path string) (string, []string, error) {
	archive, err := s.engine.ArchiveFrom(ctx, id, path)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = archive.Close() }()
	return manifest(archive)
}

// manifest reads a volume's archive, as the engine answers it for the
// volume's mount point, and answers the digest of the stored state it
// holds: each store file and everything in the outbox, by path, mode,
// owner and content, a store file or outbox that is absent recorded as
// absent. What else the volume holds at its top is answered beside the
// digest and not part of it.
func manifest(r io.Reader) (string, []string, error) {
	tr := tar.NewReader(r)
	var lines, extra []string
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, errorx.Wrap(err, "workload: archive", errorx.KindUnavailable)
		}
		_, rel, _ := strings.Cut(strings.TrimSuffix(h.Name, "/"), "/")
		top, _, _ := strings.Cut(rel, "/")
		if rel == "" {
			continue
		}
		if top != outboxDir && !slices.Contains(storeFiles, rel) {
			if top == rel {
				extra = append(extra, rel)
			}
			continue
		}
		seen[top] = true
		mode := h.FileInfo().Mode()
		entry := fmt.Sprintf("%s %s %d:%d", rel, mode, h.Uid, h.Gid)
		switch {
		case mode.IsDir() && top == outboxDir:
			lines = append(lines, "dir "+entry)
		case mode.IsRegular():
			sum := sha256.New()
			if _, err := io.Copy(sum, tr); err != nil {
				return "", nil, errorx.Wrap(err, "workload: archive", errorx.KindUnavailable)
			}
			lines = append(lines, "file "+entry+" "+hex.EncodeToString(sum.Sum(nil)))
		default:
			lines = append(lines, "other "+entry)
		}
	}
	for _, name := range append(slices.Clone(storeFiles), outboxDir) {
		if !seen[name] {
			lines = append(lines, "absent "+name)
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), extra, nil
}

// lastLine is the last line of what a container wrote.
func lastLine(said string) string {
	lines := strings.Split(strings.TrimSpace(said), "\n")
	return lines[len(lines)-1]
}
