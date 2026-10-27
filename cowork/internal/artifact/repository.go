package artifact

import "context"

// Repository keeps artifacts and their versions' records. Every method
// is scoped to an organization.
type Repository interface {
	// Create stores a and its first version.
	Create(ctx context.Context, a Artifact, v Version) error
	// AddVersion stores v as the artifact's next version, numbering it
	// itself, so two saves at once are two versions, and answers the
	// artifact as it then stands.
	AddVersion(ctx context.Context, org string, v Version) (Artifact, Version, error)
	Get(ctx context.Context, org, id string) (Artifact, error)
	// List answers the organization's artifacts, newest first, at most
	// limit.
	List(ctx context.Context, org string, limit int) ([]Artifact, error)
	// Versions answers an artifact's versions, oldest first.
	Versions(ctx context.Context, org, id string) ([]Version, error)
	Version(ctx context.Context, org, id string, number int) (Version, error)
}
