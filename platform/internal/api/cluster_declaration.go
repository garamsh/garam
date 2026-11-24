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
// acts: the caller authorized for action on the organization, the
// cluster's GRN and the declarations (ADR-0187).
func (s *Server) declarationCall(r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug, action string) (grn.GRN, cluster.DeclarationStore, error) {
	_, stores, err := s.clusterCall(r, orgSlug, action)
	if err != nil {
		return grn.GRN{}, nil, err
	}
	if stores.declarations == nil {
		return grn.GRN{}, nil, errorx.New(errorx.KindUnavailable, "declarations are not configured")
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		return grn.GRN{}, nil, err
	}
	return g, stores.declarations, nil
}

// ListClusterDeclarations answers the cluster's live declarations.
func (s *Server) ListClusterDeclarations(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	g, declarations, err := s.declarationCall(r, orgSlug, clusterSlug, org.ActionClusterRead)
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

// CreateClusterDeclaration places a new declaration under an
// identifier garam draws.
func (s *Server) CreateClusterDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	s.declare(w, r, orgSlug, clusterSlug, "")
}

// PutClusterDeclaration places a declaration under the identifier
// named, or its next generation.
func (s *Server) PutClusterDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug, id apigen.DeclarationID) {
	if err := cluster.CheckDeclarationID(id); err != nil {
		writeError(w, err)
		return
	}
	s.declare(w, r, orgSlug, clusterSlug, id)
}

// declare writes the body's object under id, or a drawn identifier
// where id is empty, answering 201 for a declaration placed and 200
// for a next generation.
func (s *Server) declare(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug, id string) {
	g, declarations, err := s.declarationCall(r, orgSlug, clusterSlug, org.ActionClusterDeclare)
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

// GetClusterDeclaration answers one live declaration.
func (s *Server) GetClusterDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug, id apigen.DeclarationID) {
	g, declarations, err := s.declarationCall(r, orgSlug, clusterSlug, org.ActionClusterRead)
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

// DeleteClusterDeclaration tombstones a declaration, which has the
// connector delete its object.
func (s *Server) DeleteClusterDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug, id apigen.DeclarationID) {
	g, declarations, err := s.declarationCall(r, orgSlug, clusterSlug, org.ActionClusterDeclare)
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
		Cluster:    d.Cluster.String(),
		Generation: d.Generation,
		Object:     json.RawMessage(d.Object.JSON()),
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}
