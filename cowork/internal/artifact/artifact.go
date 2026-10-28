// Package artifact is what people and agents publish in cowork beside
// its pages: an HTML page, or a document frozen from a page, each a
// sequence of immutable versions (ADR-0127 §8). A version's bytes are
// kept in the blob store, and an HTML version is served only from the
// user-content origin.
package artifact

import (
	"time"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// ServiceType is an artifact's GRN service type.
const ServiceType = "cowork-artifact"

// The platform's actions on artifacts: on the organization, to list
// and create them, and on an artifact, to read it and save versions.
const (
	ActionRead  = platform.ActionArtifactRead
	ActionWrite = "cowork:artifact-write"
)

// Kind is what an artifact's versions hold.
type Kind string

const (
	// KindHTML is an HTML page, run in a sandbox on the user-content
	// origin.
	KindHTML Kind = "html"
	// KindDocument is a page's blocks, frozen as they stood when saved.
	KindDocument Kind = "document"
)

func (k Kind) valid() bool { return k == KindHTML || k == KindDocument }

// The media types versions are kept under.
const (
	ContentTypeHTML     = "text/html; charset=utf-8"
	ContentTypeDocument = "application/vnd.cowork.document+json"
)

// Artifact is one artifact of an organization. Latest is the number of
// its newest version.
type Artifact struct {
	ID        string
	Org       string
	Kind      Kind
	Title     string
	Latest    int
	CreatedBy grn.GRN
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GRN is the artifact's resource name under its organization.
func (a Artifact) GRN() grn.GRN {
	return grn.New(a.Org, grn.DefaultProject, ServiceType, a.ID)
}

// Version is one saved version. Its bytes are under BlobKey.
type Version struct {
	Artifact    string
	Number      int
	BlobKey     string
	ContentType string
	Size        int64
	SHA256      string
	CreatedBy   grn.GRN
	CreatedAt   time.Time
}

// Content is what a version holds: an HTML page, or a frozen page's
// blocks.
type Content struct {
	// HTML is an HTML version's page.
	HTML string
	// Page is the ID of the page a document version freezes.
	Page string
}

// CreateInput is a new artifact and its first version.
type CreateInput struct {
	Kind    Kind
	Title   string
	Content Content
}

// Limits on what a caller sends.
const (
	maxTitle = 500
	// maxSize is the largest version, in bytes.
	maxSize = 5 << 20
	// maxListed is the most artifacts one listing answers.
	maxListed = 500
)

var (
	ErrArtifactNotFound = errorx.New(errorx.KindNotFound, "artifact not found")
	ErrVersionNotFound  = errorx.New(errorx.KindNotFound, "artifact version not found")
	ErrInvalidArtifact  = errorx.New(errorx.KindInvalidArgument,
		"an artifact is html or document, with a title of 1 to 500 bytes; an html version is a page of at most 5 MiB, and a document version names the page it freezes")
	// ErrNotServedHere is an HTML version asked of cowork's own origin,
	// which never serves one (ADR-0127 §8).
	ErrNotServedHere = errorx.New(errorx.KindFailedPrecondition, "an html artifact is served only from the user-content origin")
	// ErrNoUserContent is an HTML version viewed on a server run
	// without a user-content origin, the one place it may be served.
	ErrNoUserContent = errorx.New(errorx.KindUnavailable, "this server views no html artifacts: no user-content origin is configured")
	// ErrNoBlobStore is an artifact asked of a server run without one.
	ErrNoBlobStore = errorx.New(errorx.KindUnavailable, "this server keeps no artifacts: no blob store is configured")
)
