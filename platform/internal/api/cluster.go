package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// clusterStores is what the cluster routes read and write: the
// records, and what each record authenticates with.
type clusterStores struct {
	records     cluster.Store
	credentials cluster.CredentialStore
}

// WithClusters gives the server the stores connected clusters are kept
// in (ADR-0184). A server without them answers the cluster routes
// unavailable.
func (s *Server) WithClusters(records cluster.Store, credentials cluster.CredentialStore) *Server {
	s.clusters = &clusterStores{records: records, credentials: credentials}
	return s
}

// clusterCall is what every cluster route reads before it acts: the
// caller, authorized for action on the organization, and the stores.
// Every cluster route is decided on the organization (ADR-0184).
func (s *Server) clusterCall(r *http.Request, orgSlug apigen.Org, action string) (*user.User, *clusterStores, error) {
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
// mints (ADR-0184 decision 1), and answers its registration token. The
// two are two writes: a record whose token was not answered is
// re-registered with reissueClusterRegistrationToken.
func (s *Server) CreateCluster(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterRegister)
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
	created, err := stores.records.Create(r.Context(), c)
	if err != nil {
		writeError(w, err)
		return
	}
	registration, err := issueRegistration(r, stores, created.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, apigen.CreatedCluster{Cluster: clusterToAPI(created), Registration: registration})
}

// ReissueClusterRegistrationToken answers a new registration token,
// spending any the cluster held (ADR-0184 decisions 3 and 7).
func (s *Server) ReissueClusterRegistrationToken(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	_, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterReissue)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	registration, err := issueRegistration(r, stores, g)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, registration)
}

func issueRegistration(r *http.Request, stores *clusterStores, g grn.GRN) (apigen.ClusterRegistrationToken, error) {
	token, expiresAt, err := cluster.IssueRegistrationToken(r.Context(), stores.credentials, g)
	if err != nil {
		return apigen.ClusterRegistrationToken{}, err
	}
	return apigen.ClusterRegistrationToken{RegistrationToken: token, ExpiresAt: expiresAt}, nil
}

// ListClusterKeys answers every key the cluster held.
func (s *Server) ListClusterKeys(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	_, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterRead)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	keys, err := stores.credentials.ListKeys(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	out := apigen.ClusterKeyList{Keys: make([]apigen.ClusterKey, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, apigen.ClusterKey{Kid: k.KID, State: apigen.ClusterKeyState(k.State),
			CreatedAt: k.CreatedAt, RetiresAt: k.RetiresAt, RevokedAt: k.RevokedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeClusterKey withdraws the key and its tokens (ADR-0184 decision
// 7), on the action that withdraws an operator's key.
func (s *Server) RevokeClusterKey(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug, kid apigen.KeyID) {
	_, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterReissue)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := stores.credentials.RevokeKey(r.Context(), g, kid); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListClusters lists the organization's connected clusters.
func (s *Server) ListClusters(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	_, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterRead)
	if err != nil {
		writeError(w, err)
		return
	}
	clusters, err := stores.records.ListByOrg(r.Context(), orgGRN(orgSlug))
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
	_, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterRead)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := stores.records.Get(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clusterToAPI(c))
}

// UpdateCluster changes the cluster's name and description, and
// nothing it authenticates by.
func (s *Server) UpdateCluster(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	_, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterUpdate)
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
	c, err := stores.records.UpdateDetails(r.Context(), g, patch)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clusterToAPI(c))
}

// DeleteCluster removes the organization's record of the cluster,
// which is what its connector authenticates as (ADR-0184 decision 7).
func (s *Server) DeleteCluster(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, clusterSlug apigen.ClusterSlug) {
	_, stores, err := s.clusterCall(r, orgSlug, org.ActionClusterDelete)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := cluster.GRNOf(orgGRN(orgSlug), clusterSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := stores.records.Delete(r.Context(), g); err != nil {
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
