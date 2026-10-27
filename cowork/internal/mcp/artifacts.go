package mcp

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/artifact"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/platform"
)

type (
	createArtifactIn struct {
		Org   string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Kind  string `json:"kind" jsonschema:"html for an HTML page; document to freeze a page as it now stands"`
		Title string `json:"title" jsonschema:"what the artifact is called"`
		HTML  string `json:"html,omitempty" jsonschema:"an html artifact's page, a whole document; it runs sandboxed and can fetch nothing"`
		Page  string `json:"page,omitempty" jsonschema:"the id of the page a document freezes"`
	}
	updateArtifactIn struct {
		Org      string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Artifact string `json:"artifact" jsonschema:"the artifact's id"`
		HTML     string `json:"html,omitempty" jsonschema:"an html artifact's next version"`
		Page     string `json:"page,omitempty" jsonschema:"for a document, the page to freeze again"`
	}
	readArtifactIn struct {
		Org      string `json:"org" jsonschema:"the workspace, as list_workspaces names it"`
		Artifact string `json:"artifact" jsonschema:"the artifact's id"`
		Version  int    `json:"version,omitempty" jsonschema:"a version's number, from 1; the latest if empty"`
	}

	artifactOut struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Title  string `json:"title"`
		Latest int    `json:"latest"`
	}
	artifactsOut struct {
		Artifacts []artifactOut `json:"artifacts"`
	}
	artifactReadOut struct {
		Artifact artifactOut `json:"artifact"`
		Version  int         `json:"version"`
		// HTML is an html version's page; Markdown a document's.
		HTML     string `json:"html,omitempty"`
		Markdown string `json:"markdown,omitempty"`
	}
)

func toArtifact(a artifact.Artifact) artifactOut {
	return artifactOut{ID: a.ID, Kind: string(a.Kind), Title: a.Title, Latest: a.Latest}
}

func (t *Tools) registerArtifacts() {
	tool(t, "list_artifacts", "List artifacts",
		"The artifacts you may read, newest first.", true, t.listArtifacts)
	tool(t, "read_artifact", "Read an artifact",
		"A version of an artifact: an html one's page, or a document's Markdown.", true, t.readArtifact)
	tool(t, "create_artifact", "Create an artifact",
		"A new artifact: an HTML page, or a document freezing a page. People see an html one in a sandbox that reaches nothing outside it.", false, t.createArtifact)
	tool(t, "update_artifact", "Update an artifact",
		"Saves an artifact's next version. Every earlier version stays readable.", false, t.updateArtifact)
}

func (t *Tools) listArtifacts(ctx context.Context, c platform.Caller, in orgIn) (artifactsOut, error) {
	as, err := t.artifacts.List(ctx, c, in.Org)
	if err != nil {
		return artifactsOut{}, err
	}
	out := artifactsOut{Artifacts: make([]artifactOut, len(as))}
	for i, a := range as {
		out.Artifacts[i] = toArtifact(a)
	}
	return out, nil
}

func (t *Tools) readArtifact(ctx context.Context, c platform.Caller, in readArtifactIn) (artifactReadOut, error) {
	a, err := t.artifacts.Get(ctx, c, in.Org, in.Artifact)
	if err != nil {
		return artifactReadOut{}, err
	}
	out := artifactReadOut{Artifact: toArtifact(a)}
	if a.Kind == artifact.KindHTML {
		out.HTML, out.Version, err = t.artifacts.Source(ctx, c, in.Org, in.Artifact, in.Version)
		return out, err
	}
	d, err := t.artifacts.Document(ctx, c, in.Org, in.Artifact, in.Version)
	if err != nil {
		return artifactReadOut{}, err
	}
	out.Version = in.Version
	if out.Version == 0 {
		out.Version = a.Latest
	}
	out.Markdown = collab.Markdown(d.Blocks)
	return out, nil
}

func (t *Tools) createArtifact(ctx context.Context, c platform.Caller, in createArtifactIn) (artifactOut, error) {
	a, err := t.artifacts.Create(ctx, c, in.Org, artifact.CreateInput{
		Kind: artifact.Kind(in.Kind), Title: in.Title, Content: artifact.Content{HTML: in.HTML, Page: in.Page},
	})
	if err != nil {
		return artifactOut{}, err
	}
	return toArtifact(a), nil
}

func (t *Tools) updateArtifact(ctx context.Context, c platform.Caller, in updateArtifactIn) (artifactOut, error) {
	a, err := t.artifacts.Save(ctx, c, in.Org, in.Artifact, artifact.Content{HTML: in.HTML, Page: in.Page})
	if err != nil {
		return artifactOut{}, err
	}
	return toArtifact(a), nil
}
