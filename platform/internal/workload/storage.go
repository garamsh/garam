package workload

import (
	"context"
	"errors"

	"github.com/garamsh/garam/lib/errorx"
)

// storage makes sure both volumes are the ones this host created:
// created where the record says none was yet, and refused where one is
// gone or carries another nonce.
func (s *Supervisor) storage(ctx context.Context) error {
	for _, v := range []struct {
		rec  **volumeRecord
		kind string
	}{{&s.st.Memory, "memory"}, {&s.st.Workspace, "workspace"}} {
		if *v.rec == nil {
			rec := &volumeRecord{Name: s.cfg.Prefix + v.kind + "-" + randomHex(8), Nonce: randomHex(16)}
			if err := s.locked(func() { *v.rec = rec }); err != nil {
				return err
			}
		}
		if err := s.ensureVolume(ctx, *v.rec); err != nil {
			return err
		}
	}
	return nil
}

// ensureVolume makes sure the volume rec names is the one this host
// created: created where the record has not seen it yet, and refused
// where it is gone or carries another nonce.
func (s *Supervisor) ensureVolume(ctx context.Context, rec *volumeRecord) error {
	_, err := s.engine.InspectVolume(ctx, rec.Name)
	if errors.Is(err, ErrAbsent) && !rec.Confirmed {
		err = s.engine.CreateVolume(ctx, rec.Name, map[string]string{labelNonce: rec.Nonce})
	}
	if err != nil && !errors.Is(err, ErrAbsent) {
		return err
	}
	if err := s.verifyVolume(ctx, rec); err != nil {
		return err
	}
	if !rec.Confirmed {
		return s.locked(func() { rec.Confirmed = true })
	}
	return nil
}

// verifyVolume refuses a volume that is gone or does not carry the
// recorded nonce.
func (s *Supervisor) verifyVolume(ctx context.Context, rec *volumeRecord) error {
	v, err := s.engine.InspectVolume(ctx, rec.Name)
	if errors.Is(err, ErrAbsent) {
		return errorx.Wrap(ErrStorageMissing, rec.Name)
	}
	if err != nil {
		return err
	}
	if v.Labels[labelNonce] != rec.Nonce {
		return errorx.Wrap(ErrStorageReplaced, rec.Name)
	}
	return nil
}

// volumesOf is what each role mounts: the memory volume into the
// runtime, its outbox directory alone into the adapter, and the
// workspace volume into the workspace alone.
func (s *Supervisor) volumesOf(r Role) []VolumeMount {
	switch r {
	case RoleRuntime:
		return []VolumeMount{{Volume: s.st.Memory.Name, Target: memoryTarget}}
	case RoleAdapter:
		return []VolumeMount{{Volume: s.st.Memory.Name, Target: outboxTarget, Subpath: outboxDir}}
	}
	return []VolumeMount{{Volume: s.st.Workspace.Name, Target: workspaceTarget}}
}

func (s *Supervisor) volumeRecord(name string) *volumeRecord {
	if s.st.Memory.Name == name {
		return s.st.Memory
	}
	return s.st.Workspace
}
