package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"

	"github.com/garamsh/gaon/lib/api"

	"github.com/garamsh/garam/lib/errorx"
)

// Options are what the operator is deployed with.
type Options struct {
	// Image is the composed guest image of this release (ADR-0151 §3).
	Image string
	// GaramRoot and ControlRoot are the trust roots the guest reads:
	// garam's server root and the control service's.
	GaramRoot   []byte
	ControlRoot []byte
	// MachineURL and ControlURL are the addresses the adapter in the
	// guest reaches garam's machine listener and the control service
	// at. They may differ from the operator's own.
	MachineURL string
	ControlURL string
	// ModelKey names the Secret key of the model's credential. It is
	// optional: nil renders an agent with none.
	ModelKey *ModelKey
	// Resources are the MicroVM's requests and limits. They may be
	// empty, which leaves the choice to gaon's scheduler.
	Resources api.ResourceRequirements
	Logger    *slog.Logger
}

// Service reconciles the agents the feed names into gaon.
type Service interface {
	// Reconcile does one level-triggered pass: it reads the whole feed
	// and the objects the operator labelled, and writes what differs.
	// It holds no state a restart loses, and deletes nothing.
	Reconcile(ctx context.Context) error
}

type reportKey struct {
	epoch int64
	state State
}

type service struct {
	feed    Feed
	garam   Garam
	cluster Cluster
	opts    Options
	log     *slog.Logger
	// reported and rendered only keep the operator from repeating a
	// report it made: a restart loses them and repeats one, which is
	// harmless where a restart that lost a token is not.
	reported map[string]reportKey
	rendered map[string]string
}

// NewService returns the Service over feed, garam and cluster.
func NewService(feed Feed, garam Garam, cluster Cluster, o Options) Service {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	return &service{
		feed: feed, garam: garam, cluster: cluster, opts: o, log: log,
		reported: map[string]reportKey{}, rendered: map[string]string{},
	}
}

// snapshot is what the operator found it owns.
type snapshot struct {
	microvms map[string]*api.MicroVM
	secrets  map[string]*api.Secret
	volumes  map[string]*api.Volume
}

// read lists the three kinds and keeps what carries the operator's
// label. The client's selector already asks for that; the filter here
// is what keeps the operator from halting an object it does not own if
// the client was built without one.
func (s *service) read(ctx context.Context) (*snapshot, error) {
	snap := &snapshot{
		microvms: map[string]*api.MicroVM{},
		secrets:  map[string]*api.Secret{},
		volumes:  map[string]*api.Volume{},
	}
	ms, err := s.cluster.ListMicroVMs(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "list microvms")
	}
	for i := range ms.Items {
		if m := &ms.Items[i]; owned(m.Metadata) {
			snap.microvms[m.Metadata.Name] = m
		}
	}
	ss, err := s.cluster.ListSecrets(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "list secrets")
	}
	for i := range ss.Items {
		if o := &ss.Items[i]; owned(o.Metadata) {
			snap.secrets[o.Metadata.Name] = o
		}
	}
	vs, err := s.cluster.ListVolumes(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "list volumes")
	}
	for i := range vs.Items {
		if v := &vs.Items[i]; owned(v.Metadata) {
			snap.volumes[v.Metadata.Name] = v
		}
	}
	return snap, nil
}

func owned(m api.ObjectMeta) bool { return m.Labels[labelRole] == roleAgent }

func (s *service) Reconcile(ctx context.Context) error {
	desired, err := s.feed.Desired(ctx)
	if err != nil {
		return errorx.Wrap(err, "read the desired feed")
	}
	snap, err := s.read(ctx)
	if err != nil {
		return err
	}
	var errs []error
	fed := map[string]bool{}
	for _, d := range desired {
		fed[namesOf(d.GRN).microvm] = true
		if err := s.reconcileAgent(ctx, d, snap); err != nil {
			errs = append(errs, errorx.Wrap(err, "agent "+d.GRN.String()))
		}
	}
	for name, m := range snap.microvms {
		if fed[name] {
			continue
		}
		if err := s.halt(ctx, m); err != nil {
			errs = append(errs, errorx.Wrap(err, "halt microvm "+name))
		}
	}
	return errors.Join(errs...)
}

// halt stops the MicroVM of an agent that left the feed and keeps it
// and its Volumes: a move to another operator is the lifecycle
// authority's, and the Volumes are the agent's memory (ADR-0151 §5).
func (s *service) halt(ctx context.Context, m *api.MicroVM) error {
	if m.Spec.RunStrategy == api.RunHalted {
		return nil
	}
	m.Spec.RunStrategy = api.RunHalted
	if _, err := s.cluster.PutMicroVM(ctx, m); err != nil {
		return errorx.Wrap(err, "put microvm")
	}
	s.log.Info("halted the microvm of an agent that left the feed", slog.String("microvm", m.Metadata.Name))
	return nil
}

func (s *service) reconcileAgent(ctx context.Context, d Desired, snap *snapshot) error {
	n := namesOf(d.GRN)
	m := snap.microvms[n.microvm]
	if m == nil && d.Stopped {
		return nil
	}
	// A Volume is the agent's memory and is removed only by a person
	// (ADR-0151 §3). Where the MicroVM exists, a Volume that is missing
	// is never made again under its name: the agent would start on an
	// empty memory with nothing said.
	volumes := map[string]api.Quantity{n.memory: d.MemoryStorage, n.workspace: d.WorkspaceStorage}
	if m != nil {
		for _, name := range []string{n.memory, n.workspace} {
			if snap.volumes[name] == nil {
				return errorx.New(errorx.KindFailedPrecondition,
					"volume "+name+" is missing and its microvm exists; nothing is created for the agent")
			}
		}
	}
	secret, err := s.ensureSecret(ctx, d, snap)
	if err != nil {
		return err
	}
	if m == nil {
		for _, name := range []string{n.memory, n.workspace} {
			if snap.volumes[name] != nil {
				continue
			}
			if _, err := s.cluster.PutVolume(ctx, volumeFor(name, d, volumes[name])); err != nil {
				return errorx.Wrap(err, "put volume "+name)
			}
		}
	}
	if m == nil {
		if m, err = s.cluster.PutMicroVM(ctx, s.microVMFor(d, secret)); err != nil {
			return errorx.Wrap(err, "put microvm")
		}
	} else if want := strategyFor(d); m.Spec.RunStrategy != want {
		m.Spec.RunStrategy = want
		if m, err = s.cluster.PutMicroVM(ctx, m); err != nil {
			return errorx.Wrap(err, "put microvm")
		}
	}
	return s.report(ctx, d, m)
}

// ensureSecret returns the agent's Secret. One that exists is left
// alone: a token held only in one process is lost with it, so a Secret
// that is missing is minted again and one that is there is never minted
// over.
func (s *service) ensureSecret(ctx context.Context, d Desired, snap *snapshot) (*api.Secret, error) {
	name := namesOf(d.GRN).secret
	if secret := snap.secrets[name]; secret != nil {
		return secret, nil
	}
	creds, err := s.garam.MintTokens(ctx, d.GRN, d.Epoch)
	if err != nil {
		return nil, errorx.Wrap(err, "mint tokens")
	}
	token, err := mintPlacementToken()
	if err != nil {
		return nil, err
	}
	secret, err := s.secretFor(d, creds, token)
	if err != nil {
		return nil, err
	}
	if secret, err = s.cluster.PutSecret(ctx, secret); err != nil {
		return nil, errorx.Wrap(err, "put secret")
	}
	snap.secrets[name] = secret
	return secret, nil
}

// placementTokenBytes is the size of a placement token before it is
// hex-encoded.
const placementTokenBytes = 32

// mintPlacementToken returns 32 random bytes, hex-encoded: the form
// the Kubernetes operator mints (garam-agent-operator ADR-0042).
func mintPlacementToken() ([]byte, error) {
	raw := make([]byte, placementTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, errorx.Wrap(err, "mint placement token")
	}
	return []byte(hex.EncodeToString(raw)), nil
}

// report sends what was observed of m, once for each change, and the
// revision rendered once the MicroVM is of the revision the feed
// releases. A stopped agent reports nothing: halting is not a state the
// machine contract has.
func (s *service) report(ctx context.Context, d Desired, m *api.MicroVM) error {
	if d.Stopped {
		return nil
	}
	key := d.GRN.String()
	want := reportKey{epoch: d.Epoch, state: observe(m)}
	if s.reported[key] != want {
		err := s.garam.ReportState(ctx, d.GRN, d.Epoch, want.state)
		switch {
		case errors.Is(err, ErrStaleEpoch):
			s.log.Warn("garam holds the agent at another epoch; the report is dropped", slog.String("agent", key))
		case err != nil:
			return errorx.Wrap(err, "report state")
		}
		s.reported[key] = want
	}
	if m.Metadata.Annotations[annotationRev] != d.Revision {
		s.log.Info("the microvm is of another revision; replacing it is a later slice", slog.String("agent", key))
		return nil
	}
	if s.rendered[key] != d.Revision {
		if err := s.feed.ReportRendered(ctx, d.GRN, d.Revision); err != nil {
			return errorx.Wrap(err, "report rendered revision")
		}
		s.rendered[key] = d.Revision
	}
	return nil
}
