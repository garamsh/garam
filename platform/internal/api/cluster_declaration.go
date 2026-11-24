package api

import (
	"encoding/json"
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/org"
)

// declarationCall is what every declaration route reads before it
// acts: the instance's GRN, the caller authorized to read the instance
// on its installation, and to declare on the organization where declare
// is set (ADR-0187, until iam:bot.place replaces it in #2082), and the
// declarations.
func (s *Server) declarationCall(r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, declare bool) (grn.GRN, cluster.DeclarationStore, error) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceRead)
	if err != nil {
		return grn.GRN{}, nil, err
	}
	if declare {
		if _, _, err := s.appCall(r, orgSlug, org.ActionClusterDeclare); err != nil {
			return grn.GRN{}, nil, err
		}
	}
	if stores.declarations == nil {
		return grn.GRN{}, nil, errorx.New(errorx.KindUnavailable, "declarations are not configured")
	}
	return i.GRN, stores.declarations, nil
}

// ListInstanceDeclarations answers the instance's live declarations.
func (s *Server) ListInstanceDeclarations(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, false)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := declarations.ListDeclarations(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	out := apigen.ClusterDeclarationList{Declarations: make([]apigen.ClusterDeclaration, 0, len(list))}
	for _, d := range list {
		out.Declarations = append(out.Declarations, declarationToAPI(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateInstanceDeclaration places a new declaration under an
// identifier garam draws.
func (s *Server) CreateInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	s.declare(w, r, orgSlug, installationSlug, instanceSlug, "")
}

// PutInstanceDeclaration places a declaration under the identifier
// named, or its next generation.
func (s *Server) PutInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id apigen.DeclarationID) {
	if err := cluster.CheckDeclarationID(id); err != nil {
		writeError(w, err)
		return
	}
	s.declare(w, r, orgSlug, installationSlug, instanceSlug, id)
}

// declare writes the body's object under id, or a drawn identifier
// where id is empty, answering 201 for a declaration placed and 200
// for a next generation.
func (s *Server) declare(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id string) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, true)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.ClusterDeclarationWrite
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	d, created, err := cluster.Declare(r.Context(), declarations, g, id, body.Object)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, declarationToAPI(d))
}

// GetInstanceDeclaration answers one live declaration.
func (s *Server) GetInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id apigen.DeclarationID) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, false)
	if err != nil {
		writeError(w, err)
		return
	}
	if cluster.CheckDeclarationID(id) != nil {
		writeError(w, cluster.ErrDeclarationNotFound)
		return
	}
	d, err := declarations.GetDeclaration(r.Context(), g, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, declarationToAPI(d))
}

// DeleteInstanceDeclaration tombstones a declaration, which has the
// connector delete its object.
func (s *Server) DeleteInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id apigen.DeclarationID) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, true)
	if err != nil {
		writeError(w, err)
		return
	}
	if cluster.CheckDeclarationID(id) != nil {
		writeError(w, cluster.ErrDeclarationNotFound)
		return
	}
	if _, err := declarations.Tombstone(r.Context(), g, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func declarationToAPI(d cluster.Declaration) apigen.ClusterDeclaration {
	return apigen.ClusterDeclaration{
		Id:         d.ID,
		Instance:   d.Cluster.String(),
		Generation: d.Generation,
		Object:     json.RawMessage(d.Object.JSON()),
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}
