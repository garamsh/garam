package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// instanceCall is what a route addressing one instance reads before it
// acts: the stores and the instance's GRN, the caller authorized for
// action on the instance's installation (ADR-0192 decision 9). An
// instance of another installation is answered 404, as one that is not
// there is.
func (s *Server) instanceCall(r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, action string) (*appStores, *app.Instance, error) {
	_, stores, installation, err := s.installationCall(r, orgSlug, installationSlug, action)
	if err != nil {
		return nil, nil, err
	}
	g, err := app.InstanceGRN(orgGRN(orgSlug), instanceSlug)
	if err != nil {
		return nil, nil, err
	}
	i, err := stores.records.GetInstance(r.Context(), g)
	if err != nil {
		return nil, nil, err
	}
	if i.Installation != installation.GRN {
		return nil, nil, app.ErrInstanceNotFound
	}
	return stores, i, nil
}

// CreateInstance records an instance of the installation under an
// identifier garam mints, where its app's instance setting admits one
// more, and answers its registration token. The two are two writes: a
// record whose token was not answered is re-registered with
// reissueInstanceRegistrationToken.
func (s *Server) CreateInstance(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug) {
	u, stores, installation, err := s.installationCall(r, orgSlug, installationSlug, org.ActionInstanceRegister)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.InstanceCreation
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	details, err := app.NewDetailsPatch(body.Name, body.Description, body.Kind)
	if err != nil {
		writeError(w, err)
		return
	}
	i, err := app.NewInstance(installation, u.GRN, details)
	if err != nil {
		writeError(w, err)
		return
	}
	created, err := stores.records.CreateInstance(r.Context(), i)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.awaitMarks(r.Context(), orgGRN(orgSlug).String(),
		policyfeed.Entity{Type: policyfeed.InstanceType, ID: created.GRN.String()}); err != nil {
		writeError(w, err)
		return
	}
	registration, err := issueRegistration(r, stores, created.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, apigen.CreatedInstance{Instance: instanceToAPI(created), Registration: registration})
}

// ReissueInstanceRegistrationToken answers a new registration token,
// spending any the instance held (ADR-0184 decisions 3 and 7).
func (s *Server) ReissueInstanceRegistrationToken(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceReissue)
	if err != nil {
		writeError(w, err)
		return
	}
	registration, err := issueRegistration(r, stores, i.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, registration)
}

func issueRegistration(r *http.Request, stores *appStores, g grn.GRN) (apigen.InstanceRegistrationToken, error) {
	token, expiresAt, err := app.IssueRegistrationToken(r.Context(), stores.credentials, g)
	if err != nil {
		return apigen.InstanceRegistrationToken{}, err
	}
	return apigen.InstanceRegistrationToken{RegistrationToken: token, ExpiresAt: expiresAt}, nil
}

// ListInstanceKeys answers every key the instance held.
func (s *Server) ListInstanceKeys(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceRead)
	if err != nil {
		writeError(w, err)
		return
	}
	keys, err := stores.credentials.ListKeys(r.Context(), i.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	out := apigen.InstanceKeyList{Keys: make([]apigen.InstanceKey, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, apigen.InstanceKey{Kid: k.KID, State: apigen.InstanceKeyState(k.State),
			CreatedAt: k.CreatedAt, RetiresAt: k.RetiresAt, RevokedAt: k.RevokedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeInstanceKey withdraws the key and its tokens (ADR-0184 decision
// 7), on the action that withdraws an operator's key.
func (s *Server) RevokeInstanceKey(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, kid apigen.KeyID) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceReissue)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := stores.credentials.RevokeKey(r.Context(), i.GRN, kid); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListInstances lists the installation's instances.
func (s *Server) ListInstances(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug) {
	_, stores, installation, err := s.installationCall(r, orgSlug, installationSlug, org.ActionInstanceRead)
	if err != nil {
		writeError(w, err)
		return
	}
	instances, err := stores.records.ListInstances(r.Context(), installation.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Instance, 0, len(instances))
	for _, i := range instances {
		out = append(out, instanceToAPI(i))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetInstance answers one of the installation's instances.
func (s *Server) GetInstance(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	_, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceRead)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instanceToAPI(i))
}

// UpdateInstance changes the instance's name, description and kind,
// and nothing it authenticates by.
func (s *Server) UpdateInstance(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceUpdate)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.InstancePatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	patch, err := app.NewDetailsPatch(body.Name, body.Description, body.Kind)
	if err != nil {
		writeError(w, err)
		return
	}
	updated, err := stores.records.UpdateInstance(r.Context(), i.GRN, patch)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instanceToAPI(updated))
}

// DeleteInstance removes the instance's record, which is what it
// authenticates as (ADR-0184 decision 7).
func (s *Server) DeleteInstance(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceDelete)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := stores.records.DeleteInstance(r.Context(), i.GRN); err != nil {
		writeError(w, err)
		return
	}
	if err := s.awaitMarks(r.Context(), orgGRN(orgSlug).String(),
		policyfeed.Entity{Type: policyfeed.InstanceType, ID: i.GRN.String()}); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func instanceToAPI(i *app.Instance) apigen.Instance {
	return apigen.Instance{
		Grn:             i.GRN.String(),
		InstallationGrn: i.Installation.String(),
		Name:            i.Name,
		Description:     i.Description,
		Kind:            i.Kind,
		OrgGrn:          i.Organization.String(),
		CreatedBy:       i.CreatedBy.String(),
		CreatedAt:       i.CreatedAt,
	}
}
