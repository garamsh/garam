package app

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// Store persists the apps garam knows, the installations of them in
// each organization and the instances of each installation.
type Store interface {
	// GetApp answers the app under g, or ErrAppNotFound.
	GetApp(ctx context.Context, g grn.GRN) (*App, error)
	// ListApps returns the apps orgGRN may see, its own and every
	// public one, ordered by GRN.
	ListApps(ctx context.Context, orgGRN grn.GRN) ([]*App, error)

	// CreateInstallation records the installation and answers it as
	// stored, its incarnation drawn. ErrInstallationExists where the
	// organization holds one of the app already.
	CreateInstallation(ctx context.Context, i *Installation) (*Installation, error)
	// GetInstallation answers the installation under g, or
	// ErrInstallationNotFound.
	GetInstallation(ctx context.Context, g grn.GRN) (*Installation, error)
	// ListInstallations returns the organization's installations,
	// ordered by GRN.
	ListInstallations(ctx context.Context, orgGRN grn.GRN) ([]*Installation, error)
	// DeleteInstallation removes the installation with its instances,
	// their keys and their tokens, and answers the instances it removed,
	// or answers ErrInstallationNotFound.
	DeleteInstallation(ctx context.Context, g grn.GRN) ([]grn.GRN, error)

	// CreateInstance records the instance and answers it as stored,
	// where the instance setting of its installation's app admits one
	// more (Instances.Admit), read while the installation's row is
	// held. ErrInstallationNotFound where the installation is gone.
	CreateInstance(ctx context.Context, i *Instance) (*Instance, error)
	// GetInstance answers the instance under g, or ErrInstanceNotFound.
	GetInstance(ctx context.Context, g grn.GRN) (*Instance, error)
	// ListInstances returns the installation's instances, ordered by
	// GRN.
	ListInstances(ctx context.Context, installation grn.GRN) ([]*Instance, error)
	// UpdateInstance applies patch to the instance's name, description
	// and kind and answers the instance as stored, or
	// ErrInstanceNotFound. It writes nothing keyed on the GRN.
	UpdateInstance(ctx context.Context, g grn.GRN, patch DetailsPatch) (*Instance, error)
	// DeleteInstance removes the record, or answers
	// ErrInstanceNotFound.
	DeleteInstance(ctx context.Context, g grn.GRN) error
}
