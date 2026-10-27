package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/cowork/api"
	"github.com/garamsh/garam/cowork/internal/artifact"
)

func artifactToAPI(a artifact.Artifact) apigen.Artifact {
	return apigen.Artifact{
		Id: a.ID, Grn: a.GRN().String(), Org: a.Org, Kind: apigen.ArtifactKind(a.Kind), Title: a.Title,
		Latest: a.Latest, CreatedBy: a.CreatedBy.String(), CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

func versionOf(q *apigen.VersionQuery) int {
	if q == nil {
		return 0
	}
	return *q
}

// ListArtifacts implements apigen.ServerInterface.
func (s *Server) ListArtifacts(w http.ResponseWriter, r *http.Request, org apigen.OrgPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	as, err := s.artifacts.List(r.Context(), c, org)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := apigen.ArtifactList{Artifacts: make([]apigen.Artifact, len(as))}
	for i, a := range as {
		out.Artifacts[i] = artifactToAPI(a)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateArtifact implements apigen.ServerInterface.
func (s *Server) CreateArtifact(w http.ResponseWriter, r *http.Request, org apigen.OrgPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.ArtifactCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	a, err := s.artifacts.Create(r.Context(), c, org, artifact.CreateInput{
		Kind: artifact.Kind(body.Kind), Title: body.Title,
		Content: artifact.Content{HTML: deref(body.Html), Page: deref(body.Page)},
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, artifactToAPI(a))
}

// GetArtifact implements apigen.ServerInterface.
func (s *Server) GetArtifact(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ArtifactPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a, err := s.artifacts.Get(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactToAPI(a))
}

// ListArtifactVersions implements apigen.ServerInterface.
func (s *Server) ListArtifactVersions(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ArtifactPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	vs, err := s.artifacts.Versions(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := apigen.ArtifactVersionList{Versions: make([]apigen.ArtifactVersion, len(vs))}
	for i, v := range vs {
		out.Versions[i] = apigen.ArtifactVersion{
			Number: v.Number, Size: v.Size, Sha256: v.SHA256, CreatedBy: v.CreatedBy.String(), CreatedAt: v.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// SaveArtifactVersion implements apigen.ServerInterface.
func (s *Server) SaveArtifactVersion(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ArtifactPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.ArtifactContent
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	a, err := s.artifacts.Save(r.Context(), c, org, id, artifact.Content{HTML: deref(body.Html), Page: deref(body.Page)})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, artifactToAPI(a))
}

// ViewArtifact implements apigen.ServerInterface.
func (s *Server) ViewArtifact(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ArtifactPath, params apigen.ViewArtifactParams) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a, err := s.artifacts.Get(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	number := versionOf(params.Version)
	if number == 0 {
		number = a.Latest
	}
	url, err := s.artifacts.View(r.Context(), c, org, id, number)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apigen.ArtifactView{Url: url, Version: number})
}

// GetArtifactDocument implements apigen.ServerInterface.
func (s *Server) GetArtifactDocument(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.ArtifactPath, params apigen.GetArtifactDocumentParams) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := s.artifacts.Document(r.Context(), c, org, id, versionOf(params.Version))
	if err != nil {
		writeError(w, r, err)
		return
	}
	blocks := make([]map[string]any, len(d.Blocks))
	for i, b := range d.Blocks {
		m := map[string]any{"type": b.Type, "attrs": b.Attrs, "text": b.Text}
		if len(b.Items) > 0 {
			m["items"] = b.Items
		}
		blocks[i] = m
	}
	writeJSON(w, http.StatusOK, apigen.ArtifactDocument{Title: d.Title, Page: d.Page, Blocks: blocks})
}
