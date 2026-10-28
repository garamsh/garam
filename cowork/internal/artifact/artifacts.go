package artifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/garamsh/garam/cowork/internal/blob"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
)

const (
	idAlphabet = "abcdefghijklmnopqrstuvwxyz234567"
	idLength   = 12
)

func newID() string {
	b := make([]byte, idLength)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return string(b)
}

// maxChecksPerAsk is how many pairs the platform answers in one
// permission check request.
const maxChecksPerAsk = 50

func onArtifact(action, id string) platform.Check {
	return platform.Check{Action: action, Kind: platform.KindArtifact, ID: id}
}

// encode answers what a version of kind keeps for content, reading a
// frozen page as the caller.
func (s *service) encode(ctx context.Context, caller platform.Caller, org string, kind Kind, content Content) (blob.Blob, error) {
	switch kind {
	case KindHTML:
		if content.HTML == "" || content.Page != "" || len(content.HTML) > maxSize {
			return blob.Blob{}, ErrInvalidArtifact
		}
		return blob.Blob{ContentType: ContentTypeHTML, Body: []byte(content.HTML)}, nil
	case KindDocument:
		if content.Page == "" || content.HTML != "" {
			return blob.Blob{}, ErrInvalidArtifact
		}
		p, err := s.pages.Get(ctx, caller, org, content.Page)
		if err != nil {
			return blob.Blob{}, err
		}
		blocks, err := s.documents.Read(ctx, collab.Room{Org: org, Page: p.ID})
		if err != nil {
			return blob.Blob{}, err
		}
		if blocks == nil {
			blocks = []collab.Block{}
		}
		body, err := json.Marshal(Document{Title: p.Title, Page: p.ID, Blocks: blocks})
		if err != nil {
			return blob.Blob{}, errorx.Wrap(err, "artifact: document", errorx.KindInternal)
		}
		if len(body) > maxSize {
			return blob.Blob{}, ErrInvalidArtifact
		}
		return blob.Blob{ContentType: ContentTypeDocument, Body: body}, nil
	}
	return blob.Blob{}, ErrInvalidArtifact
}

// keep puts b in the blob store under a key of its own and answers
// the version that records it. A version's key names a random part, so
// two saves at once never meet in the store; the repository numbers
// them.
func (s *service) keep(ctx context.Context, caller platform.Caller, org, id string, b blob.Blob) (Version, error) {
	key := org + "/" + id + "/" + rand.Text()
	if err := s.blobs.Put(ctx, key, b); err != nil {
		return Version{}, err
	}
	sum := sha256.Sum256(b.Body)
	return Version{
		Artifact: id, BlobKey: key, ContentType: b.ContentType, Size: int64(len(b.Body)),
		SHA256: hex.EncodeToString(sum[:]), CreatedBy: caller.GRN, CreatedAt: time.Now(),
	}, nil
}

// Create adds an artifact with its first version, as the caller.
func (s *service) Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Artifact, error) {
	if s.blobs == nil {
		return Artifact{}, ErrNoBlobStore
	}
	title := strings.TrimSpace(in.Title)
	if !in.Kind.valid() || title == "" || len(title) > maxTitle {
		return Artifact{}, ErrInvalidArtifact
	}
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: ActionWrite}); err != nil {
		return Artifact{}, err
	}
	b, err := s.encode(ctx, caller, org, in.Kind, in.Content)
	if err != nil {
		return Artifact{}, err
	}
	a := Artifact{ID: newID(), Org: org, Kind: in.Kind, Title: title, Latest: 1, CreatedBy: caller.GRN}
	v, err := s.keep(ctx, caller, org, a.ID, b)
	if err != nil {
		return Artifact{}, err
	}
	v.Number = 1
	a.CreatedAt, a.UpdatedAt = v.CreatedAt, v.CreatedAt
	if err := s.repo.Create(ctx, a, v); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// Save keeps content as the next version of an artifact the caller
// may write. Every save is kept, and none replaces another.
func (s *service) Save(ctx context.Context, caller platform.Caller, org, id string, content Content) (Artifact, error) {
	if s.blobs == nil {
		return Artifact{}, ErrNoBlobStore
	}
	if err := platform.Require(ctx, s.platform, caller, org, onArtifact(ActionWrite, id)); err != nil {
		return Artifact{}, err
	}
	a, err := s.repo.Get(ctx, org, id)
	if err != nil {
		return Artifact{}, err
	}
	b, err := s.encode(ctx, caller, org, a.Kind, content)
	if err != nil {
		return Artifact{}, err
	}
	v, err := s.keep(ctx, caller, org, id, b)
	if err != nil {
		return Artifact{}, err
	}
	a, _, err = s.repo.AddVersion(ctx, org, v)
	return a, err
}

// Get answers an artifact the caller may read.
func (s *service) Get(ctx context.Context, caller platform.Caller, org, id string) (Artifact, error) {
	if err := platform.Require(ctx, s.platform, caller, org, onArtifact(ActionRead, id)); err != nil {
		return Artifact{}, err
	}
	return s.repo.Get(ctx, org, id)
}

// List answers the artifacts the caller may read.
func (s *service) List(ctx context.Context, caller platform.Caller, org string) ([]Artifact, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: ActionRead}); err != nil {
		return nil, err
	}
	listed, err := s.repo.List(ctx, org, maxListed)
	if err != nil {
		return nil, err
	}
	out := []Artifact{}
	for start := 0; start < len(listed); start += maxChecksPerAsk {
		batch := listed[start:min(start+maxChecksPerAsk, len(listed))]
		checks := make([]platform.Check, len(batch))
		for i, a := range batch {
			checks[i] = onArtifact(ActionRead, a.ID)
		}
		allowed, err := s.platform.Allowed(ctx, caller, org, checks)
		if err != nil {
			return nil, err
		}
		for i, a := range batch {
			if allowed[i] {
				out = append(out, a)
			}
		}
	}
	return out, nil
}

// Versions answers every version of an artifact the caller may read.
func (s *service) Versions(ctx context.Context, caller platform.Caller, org, id string) ([]Version, error) {
	if _, err := s.Get(ctx, caller, org, id); err != nil {
		return nil, err
	}
	return s.repo.Versions(ctx, org, id)
}

// version answers the artifact the caller may read and its version
// number, the latest for 0.
func (s *service) version(ctx context.Context, caller platform.Caller, org, id string, number int) (Artifact, Version, error) {
	a, err := s.Get(ctx, caller, org, id)
	if err != nil {
		return Artifact{}, Version{}, err
	}
	if number == 0 {
		number = a.Latest
	}
	v, err := s.repo.Version(ctx, org, id, number)
	return a, v, err
}

// Document answers a document version.
func (s *service) Document(ctx context.Context, caller platform.Caller, org, id string, number int) (Document, error) {
	if s.blobs == nil {
		return Document{}, ErrNoBlobStore
	}
	a, v, err := s.version(ctx, caller, org, id, number)
	if err != nil {
		return Document{}, err
	}
	if a.Kind != KindDocument {
		return Document{}, ErrNotServedHere
	}
	b, err := s.blobs.Get(ctx, v.BlobKey)
	if err != nil {
		return Document{}, err
	}
	var d Document
	if err := json.Unmarshal(b.Body, &d); err != nil {
		return Document{}, errorx.Wrap(err, "artifact: stored document", errorx.KindDataLoss)
	}
	return d, nil
}

// View answers where an HTML version the caller may read is viewed.
func (s *service) View(ctx context.Context, caller platform.Caller, org, id string, number int) (string, error) {
	a, v, err := s.version(ctx, caller, org, id, number)
	if err != nil {
		return "", err
	}
	if a.Kind != KindHTML {
		return "", ErrInvalidArtifact
	}
	if s.viewer == nil {
		return "", ErrNoUserContent
	}
	return s.viewer.URL(org, id, v.Number), nil
}

// Source answers an HTML version the caller may read, and its number.
func (s *service) Source(ctx context.Context, caller platform.Caller, org, id string, number int) (string, int, error) {
	if s.blobs == nil {
		return "", 0, ErrNoBlobStore
	}
	a, v, err := s.version(ctx, caller, org, id, number)
	if err != nil {
		return "", 0, err
	}
	if a.Kind != KindHTML {
		return "", 0, ErrInvalidArtifact
	}
	b, err := s.blobs.Get(ctx, v.BlobKey)
	if err != nil {
		return "", 0, err
	}
	return string(b.Body), v.Number, nil
}

// Served answers an HTML version's bytes, asked by number.
func (s *service) Served(ctx context.Context, org, id string, number int) (blob.Blob, error) {
	if s.blobs == nil {
		return blob.Blob{}, ErrNoBlobStore
	}
	a, err := s.repo.Get(ctx, org, id)
	if err != nil {
		return blob.Blob{}, err
	}
	if a.Kind != KindHTML {
		return blob.Blob{}, ErrVersionNotFound
	}
	v, err := s.repo.Version(ctx, org, id, number)
	if err != nil {
		return blob.Blob{}, err
	}
	return s.blobs.Get(ctx, v.BlobKey)
}
