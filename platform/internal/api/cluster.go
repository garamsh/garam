package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// WithClusters gives the server the store connected clusters are kept
// in (ADR-0184). A server without one answers the cluster routes
// unavailable.
func (s *Server) WithClusters(store cluster.Store) *Server {
	s.clusters = store
	return s
}

// clusterCall is what every cluster route reads before it acts: the
// caller, authorized for action on the organization, and the store.
// Every cluster route is decided on the organization (ADR-0184).
func (s *Server) clusterCall(r *http.Request, orgSlug apigen.Org, action string) (*user.User, cluster.Store, error) {
	if s.clusters == nil {
		return nil, nil, errorx.New(errorx.KindUnavailable, "clusters are not configured")
	}
	u, err := s.currentUser(r)
	if err != nil {
		return nil, nil, err
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, action); err != nil {
		return nil, nil, err
	}
	return u, s.clusters, nil
}

// CreateCluster records a connected cluster under an identifier garam
// mints (ADR-0184 decision 1).
func (s *Server) CreateCluster(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, store, err := s.clusterCall(r, orgSlug, org.ActionClusterRegister)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.ClusterCreation
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	details, err := cluster.NewDetailsPatch(body.Name, body.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := cluster.New(orgGRN(orgSlug), u.GRN, details)
	if err != nil {
		writeError(w, err)
		return
	}
	created, err := store.Create(r.Context(), c)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, clusterToAPI(created))
}

// ListClusters lists the organization's connected clusters.
func (s *Server) ListClusters(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	_, store, err := s.clusterCall(r, orgSlug, org.ActionClusterRead)
	if err != nil {
		writeError(w, err)
		return
	}
	clusters, err := store.ListByOrg(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Cluster, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, clusterToAPI(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetCluster answers one of the organization's connected clusters.
func (s *Server) GetCluster(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	_, store, err := s.clusterCall(r, orgSlug, org.ActionClusterRead)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := store.Get(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clusterToAPI(c))
}

// UpdateCluster changes the cluster's name and description, and
// nothing it authenticates by.
func (s *Server) UpdateCluster(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	_, store, err := s.clusterCall(r, orgSlug, org.ActionClusterUpdate)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.ClusterPatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	patch, err := cluster.NewDetailsPatch(body.Name, body.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := store.UpdateDetails(r.Context(), g, patch)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clusterToAPI(c))
}

// DeleteCluster removes the organization's record of the cluster,
// which is what its connector authenticates as (ADR-0184 decision 7).
func (s *Server) DeleteCluster(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	_, store, err := s.clusterCall(r, orgSlug, org.ActionClusterDelete)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := store.Delete(r.Context(), g); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func clusterToAPI(c *cluster.Cluster) apigen.Cluster {
	return apigen.Cluster{
		Grn:         c.GRN.String(),
		Name:        c.Name,
		Description: c.Description,
		OrgGrn:      c.Organization.String(),
		CreatedBy:   c.CreatedBy.String(),
		CreatedAt:   c.CreatedAt,
	}
}
