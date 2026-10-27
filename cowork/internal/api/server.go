// Package api serves cowork's HTTP surface: the page tree generated
// from api/openapi.yaml, and each page's real-time sync over a
// WebSocket.
package api

import (
	"encoding/json"
	"net/http"

	apigen "github.com/garamsh/garam/cowork/api"
	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// Server implements the generated apigen.ServerInterface and the sync
// endpoint beside it.
type Server struct {
	pages    page.Service
	collab   collab.Service
	channels channel.Service
	platform platform.Platform
	// origins are the host patterns a browser may open a sync
	// WebSocket from, besides cowork's own host.
	origins []string
}

var _ apigen.ServerInterface = (*Server)(nil)

// NewServer answers the surface over its services.
func NewServer(pages page.Service, collabs collab.Service, channels channel.Service, p platform.Platform, origins []string) *Server {
	return &Server{pages: pages, collab: collabs, channels: channels, platform: p, origins: origins}
}

// caller identifies the request's caller with the platform.
func (s *Server) caller(r *http.Request) (platform.Caller, error) {
	return s.platform.Identify(r.Context(), credentialOf(r))
}

func pageToAPI(p page.Page) apigen.Page {
	return apigen.Page{
		Id:        p.ID,
		Grn:       p.GRN().String(),
		Org:       p.Org,
		Parent:    p.Parent,
		Title:     p.Title,
		Position:  p.Position,
		CreatedBy: p.CreatedBy.String(),
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func decodeJSON(r *http.Request, into any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return errorx.Wrap(err, "request body", errorx.KindInvalidArgument)
	}
	return nil
}

// ListPages implements apigen.ServerInterface.
func (s *Server) ListPages(w http.ResponseWriter, r *http.Request, org apigen.OrgPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	pages, err := s.pages.List(r.Context(), c, org)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := apigen.PageList{Pages: make([]apigen.Page, len(pages))}
	for i, p := range pages {
		out.Pages[i] = pageToAPI(p)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreatePage implements apigen.ServerInterface.
func (s *Server) CreatePage(w http.ResponseWriter, r *http.Request, org apigen.OrgPath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.PageCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	in := page.CreateInput{Title: body.Title}
	if body.Parent != nil {
		in.Parent = *body.Parent
	}
	p, err := s.pages.Create(r.Context(), c, org, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, pageToAPI(p))
}

// GetPage implements apigen.ServerInterface.
func (s *Server) GetPage(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.PagePath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := s.pages.Get(r.Context(), c, org, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageToAPI(p))
}

// UpdatePage implements apigen.ServerInterface.
func (s *Server) UpdatePage(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.PagePath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body apigen.PageUpdate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	p, err := s.pages.Update(r.Context(), c, org, id, page.UpdateInput{Title: body.Title, Parent: body.Parent, Position: body.Position})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pageToAPI(p))
}

// DeletePage implements apigen.ServerInterface.
func (s *Server) DeletePage(w http.ResponseWriter, r *http.Request, org apigen.OrgPath, id apigen.PagePath) {
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.pages.Delete(r.Context(), c, org, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
