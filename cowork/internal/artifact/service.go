package artifact

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/blob"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/platform"
)

// Service is artifacts as a caller acts on them. Listing needs
// cowork:artifact-read on the organization and on each artifact
// listed, creating cowork:artifact-write on the organization, reading
// an artifact or any version cowork:artifact-read on it, and saving a
// version cowork:artifact-write on it. Freezing a page needs the
// caller to read the page.
type Service interface {
	Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Artifact, error)
	// Save keeps content as the artifact's next version.
	Save(ctx context.Context, caller platform.Caller, org, id string, content Content) (Artifact, error)
	Get(ctx context.Context, caller platform.Caller, org, id string) (Artifact, error)
	List(ctx context.Context, caller platform.Caller, org string) ([]Artifact, error)
	Versions(ctx context.Context, caller platform.Caller, org, id string) ([]Version, error)
	// Document answers a document version, 0 for the latest.
	Document(ctx context.Context, caller platform.Caller, org, id string, number int) (Document, error)
	// View answers where an HTML version is viewed, 0 for the latest.
	View(ctx context.Context, caller platform.Caller, org, id string, number int) (string, error)
	// Source answers an HTML version's page as data, never as a page
	// cowork serves: for a tool that edits it. 0 is the latest.
	Source(ctx context.Context, caller platform.Caller, org, id string, number int) (string, int, error)
	// Served answers an HTML version's bytes for the user-content
	// origin, whose address already proves the viewer was allowed it.
	Served(ctx context.Context, org, id string, number int) (blob.Blob, error)
}

// Document is a document version: the page it froze, as it stood.
type Document struct {
	Title  string         `json:"title"`
	Page   string         `json:"page"`
	Blocks []collab.Block `json:"blocks"`
}

type service struct {
	repo      Repository
	blobs     blob.Store
	platform  platform.Platform
	pages     Pages
	documents Documents
	viewer    Viewer
}

// NewService answers artifacts over repo, their bytes in blobs, deciding
// every act through p. blobs nil is a server that keeps no artifacts,
// and answers every call ErrNoBlobStore.
func NewService(repo Repository, blobs blob.Store, p platform.Platform, pages Pages, documents Documents, viewer Viewer) Service {
	return &service{repo: repo, blobs: blobs, platform: p, pages: pages, documents: documents, viewer: viewer}
}
