package cluster

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// Store persists the clusters an organization has created.
type Store interface {
	// Create records the cluster and answers it as stored.
	Create(ctx context.Context, c *Cluster) (*Cluster, error)
	// Get answers the cluster under g, or ErrClusterNotFound.
	Get(ctx context.Context, g grn.GRN) (*Cluster, error)
	// ListByOrg returns the organization's clusters, ordered by GRN.
	ListByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Cluster, error)
	// UpdateDetails applies patch to the cluster's name and description
	// and answers the cluster as stored, or ErrClusterNotFound. It
	// writes nothing keyed on the GRN.
	UpdateDetails(ctx context.Context, g grn.GRN, patch DetailsPatch) (*Cluster, error)
	// Delete removes the record, or answers ErrClusterNotFound.
	Delete(ctx context.Context, g grn.GRN) error
}
