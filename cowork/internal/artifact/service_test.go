package artifact_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/artifact"
	"github.com/garamsh/garam/cowork/internal/artifact/memory"
	blobmemory "github.com/garamsh/garam/cowork/internal/blob/memory"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/platform/sdk/errorx"
	"github.com/garamsh/garam/platform/sdk/grn"
)

type policy struct{ deny map[string]bool }

func (p *policy) Identify(context.Context, platform.Credential) (platform.Caller, error) {
	return platform.Caller{}, nil
}

func (p *policy) Organizations(context.Context, platform.Caller) ([]platform.Organization, error) {
	return nil, nil
}

func (p *policy) Allowed(_ context.Context, c platform.Caller, _ string, checks []platform.Check) ([]bool, error) {
	out := make([]bool, len(checks))
	for i, ch := range checks {
		k := ch.Action
		if ch.ID != "" {
			k += "@" + ch.ID
		}
		out[i] = !p.deny[k] && !p.deny[c.GRN.Identifier+" "+k]
	}
	return out, nil
}

// pages holds one page, "plan", which carol may not read; its blocks
// are whatever text holds when it is read.
type pages struct{ text string }

func (p *pages) Get(_ context.Context, c platform.Caller, _ string, id string) (page.Page, error) {
	if id != "plan" {
		return page.Page{}, page.ErrPageNotFound
	}
	if c.GRN.Identifier == "carol" {
		return page.Page{}, platform.ErrNotAllowed
	}
	return page.Page{ID: id, Title: "Plan"}, nil
}

func (p *pages) Read(context.Context, collab.Room) ([]collab.Block, error) {
	return []collab.Block{{Type: "paragraph", Text: p.text}}, nil
}

type viewer struct{}

func (viewer) URL(org, id string, n int) string {
	return "https://content.example/a/" + org + "/" + id + "/" + strconv.Itoa(n)
}

func person(name string) platform.Caller {
	return platform.Caller{GRN: grn.New(grn.RootOwner, grn.DefaultProject, "user", name)}
}

var (
	ada   = person("ada")
	bob   = person("bob")
	carol = person("carol")
	ctx   = context.Background()
)

func newService(deny map[string]bool, p *pages) artifact.Service {
	return artifact.NewService(memory.New(), blobmemory.New(), &policy{deny: deny}, p, p, viewer{})
}

// TestSave_KeepsEveryVersionAndEachReadsBack saves an HTML artifact
// three times; every version stays, numbered in order, each served as
// it was saved.
func TestSave_KeepsEveryVersionAndEachReadsBack(t *testing.T) {
	s := newService(nil, &pages{})
	a, err := s.Create(ctx, ada, "acme", artifact.CreateInput{Kind: artifact.KindHTML, Title: "Chart", Content: artifact.Content{HTML: "<p>1</p>"}})
	require.NoError(t, err)
	assert.Equal(t, 1, a.Latest)
	for _, html := range []string{"<p>2</p>", "<p>3</p>"} {
		a, err = s.Save(ctx, bob, "acme", a.ID, artifact.Content{HTML: html})
		require.NoError(t, err)
	}
	assert.Equal(t, 3, a.Latest)

	vs, err := s.Versions(ctx, ada, "acme", a.ID)
	require.NoError(t, err)
	require.Len(t, vs, 3)
	for i, v := range vs {
		assert.Equal(t, i+1, v.Number)
		b, err := s.Served(ctx, "acme", a.ID, v.Number)
		require.NoError(t, err)
		assert.Equal(t, "<p>"+strconv.Itoa(i+1)+"</p>", string(b.Body))
	}
	assert.Equal(t, bob.GRN, vs[2].CreatedBy)

	url, err := s.View(ctx, ada, "acme", a.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, "https://content.example/a/acme/"+a.ID+"/3", url, "0 views the latest")
}

// TestDocument_FreezesThePageAsItStood: a document version keeps the
// page's blocks when saved, and a later edit to the page reaches only
// the next version.
func TestDocument_FreezesThePageAsItStood(t *testing.T) {
	p := &pages{text: "first draft"}
	s := newService(nil, p)
	a, err := s.Create(ctx, ada, "acme", artifact.CreateInput{Kind: artifact.KindDocument, Title: "Plan v1", Content: artifact.Content{Page: "plan"}})
	require.NoError(t, err)
	p.text = "second draft"
	_, err = s.Save(ctx, ada, "acme", a.ID, artifact.Content{Page: "plan"})
	require.NoError(t, err)

	first, err := s.Document(ctx, ada, "acme", a.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "first draft", first.Blocks[0].Text)
	latest, err := s.Document(ctx, ada, "acme", a.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, "second draft", latest.Blocks[0].Text)
	assert.Equal(t, "Plan", latest.Title)

	_, err = s.Create(ctx, carol, "acme", artifact.CreateInput{Kind: artifact.KindDocument, Title: "x", Content: artifact.Content{Page: "plan"}})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "carol may not read the page")
}

// TestArtifacts_ThePlatformDecides: a writer it denies saves nothing,
// a reader it denies reads no version and finds none listed.
func TestArtifacts_ThePlatformDecides(t *testing.T) {
	deny := map[string]bool{}
	s := newService(deny, &pages{})
	a, err := s.Create(ctx, ada, "acme", artifact.CreateInput{Kind: artifact.KindHTML, Title: "Chart", Content: artifact.Content{HTML: "<p>1</p>"}})
	require.NoError(t, err)
	deny["bob cowork:artifact-write@"+a.ID] = true
	deny["carol cowork:artifact-read@"+a.ID] = true

	_, err = s.Save(ctx, bob, "acme", a.ID, artifact.Content{HTML: "<p>2</p>"})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	got, err := s.Get(ctx, ada, "acme", a.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Latest, "the refused save kept nothing")

	_, err = s.View(ctx, carol, "acme", a.ID, 1)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	listed, err := s.List(ctx, carol, "acme")
	require.NoError(t, err)
	assert.Empty(t, listed)
	listed, err = s.List(ctx, bob, "acme")
	require.NoError(t, err)
	assert.Len(t, listed, 1)

	deny["bob cowork:artifact-write"] = true
	_, err = s.Create(ctx, bob, "acme", artifact.CreateInput{Kind: artifact.KindHTML, Title: "x", Content: artifact.Content{HTML: "x"}})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
}

// TestArtifacts_KeepHTMLOffCoworksOrigin: a document is read on
// cowork's origin; an HTML version is not, and has only a view.
func TestArtifacts_KeepHTMLOffCoworksOrigin(t *testing.T) {
	s := newService(nil, &pages{})
	a, err := s.Create(ctx, ada, "acme", artifact.CreateInput{Kind: artifact.KindHTML, Title: "Chart", Content: artifact.Content{HTML: "<script>1</script>"}})
	require.NoError(t, err)
	_, err = s.Document(ctx, ada, "acme", a.ID, 1)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition), "%v", err)
}

// TestCreate_RefusesWhatItCannotKeep, and a server without a blob
// store keeps nothing.
func TestCreate_RefusesWhatItCannotKeep(t *testing.T) {
	s := newService(nil, &pages{})
	for name, in := range map[string]artifact.CreateInput{
		"kind":        {Kind: "pdf", Title: "x", Content: artifact.Content{HTML: "x"}},
		"title":       {Kind: artifact.KindHTML, Title: " ", Content: artifact.Content{HTML: "x"}},
		"empty html":  {Kind: artifact.KindHTML, Title: "x"},
		"no page":     {Kind: artifact.KindDocument, Title: "x"},
		"large":       {Kind: artifact.KindHTML, Title: "x", Content: artifact.Content{HTML: string(make([]byte, 5<<20+1))}},
		"both bodies": {Kind: artifact.KindHTML, Title: "x", Content: artifact.Content{HTML: "x", Page: "plan"}},
	} {
		_, err := s.Create(ctx, ada, "acme", in)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), name)
	}

	none := artifact.NewService(memory.New(), nil, &policy{}, &pages{}, &pages{}, viewer{})
	_, err := none.Create(ctx, ada, "acme", artifact.CreateInput{Kind: artifact.KindHTML, Title: "x", Content: artifact.Content{HTML: "x"}})
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
}
