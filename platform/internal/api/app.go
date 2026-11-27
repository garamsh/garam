package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	"github.com/garamsh/garam/platform/internal/policyrelay"
	"github.com/garamsh/garam/platform/internal/user"
)

// appStores is what the app system's routes read and write: the apps,
// installations and instances, what each instance authenticates with,
// and what is declared on it and what it reported.
type appStores struct {
	records      app.Store
	credentials  app.CredentialStore
	declarations cluster.DeclarationStore
	reports      cluster.ReportStore
	// declared is what the manifests declare, which an app's requested
	// actions are checked against (ADR-0192 decision 6).
	declared []grn.ActionInfo
}

// WithApps gives the server the stores the app system keeps (ADR-0192),
// the declarations placed on an instance (ADR-0187) and what it reports
// (ADR-0188). A server without them answers the app system's routes
// unavailable, one with nil declarations the declaration routes, and
// one with nil reports the report route.
func (s *Server) WithApps(records app.Store, credentials app.CredentialStore, declarations cluster.DeclarationStore, reports cluster.ReportStore) *Server {
	s.apps = &appStores{records: records, credentials: credentials, declarations: declarations, reports: reports,
		declared: grn.Actions()}
	return s
}

// appCall is what a route decided on the organization reads before it
// acts: the caller, authorized for action there, and the stores.
func (s *Server) appCall(r *http.Request, orgSlug apigen.Org, action string) (*user.User, *appStores, error) {
	if s.apps == nil {
		return nil, nil, errorx.New(errorx.KindUnavailable, "apps are not configured")
	}
	u, err := s.currentUser(r)
	if err != nil {
		return nil, nil, err
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, action); err != nil {
		return nil, nil, err
	}
	return u, s.apps, nil
}

// installationCall is what a route decided on an installation reads
// before it acts: the caller, the installation, and the caller
// authorized for action on it (ADR-0192 decision 9). An installation
// the organization does not hold is answered 404 before the decision.
func (s *Server) installationCall(r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, action string) (*user.User, *appStores, *app.Installation, error) {
	if s.apps == nil {
		return nil, nil, nil, errorx.New(errorx.KindUnavailable, "apps are not configured")
	}
	u, err := s.currentUser(r)
	if err != nil {
		return nil, nil, nil, err
	}
	g, err := app.InstallationGRN(orgGRN(orgSlug), installationSlug)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := s.callerIn(r, orgGRN(orgSlug), u); err != nil {
		return nil, nil, nil, err
	}
	installation, err := s.apps.records.GetInstallation(r.Context(), g)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), g, u, action); err != nil {
		return nil, nil, nil, err
	}
	return u, s.apps, installation, nil
}

// ListApps answers the apps the organization may install.
func (s *Server) ListApps(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	_, stores, err := s.appCall(r, orgSlug, org.ActionAppRead)
	if err != nil {
		writeError(w, err)
		return
	}
	apps, err := stores.records.ListApps(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.App, 0, len(apps))
	for _, a := range apps {
		out = append(out, appToAPI(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetApp answers one app the organization may install, by its slug.
func (s *Server) GetApp(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, appSlug apigen.AppSlug) {
	_, stores, err := s.appCall(r, orgSlug, org.ActionAppRead)
	if err != nil {
		writeError(w, err)
		return
	}
	a, err := visibleApp(r, stores, orgGRN(orgSlug), appSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, appToAPI(a))
}

// visibleApp answers the app under slug where orgGRN may see it: its
// own, or a public one. Any other is ErrAppNotFound, as one that is not
// there is. A slug is unique across every owner, so it names one app.
func visibleApp(r *http.Request, stores *appStores, orgGRN grn.GRN, slug string) (*app.App, error) {
	apps, err := stores.records.ListApps(r.Context(), orgGRN)
	if err != nil {
		return nil, err
	}
	for _, a := range apps {
		if a.Slug == slug {
			return a, nil
		}
	}
	return nil, app.ErrAppNotFound
}

// CreateInstallation installs an app in the organization over the scope
// the body names, writing a `permit` of each action the app requests
// (ADR-0192 decision 4).
func (s *Server) CreateInstallation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, stores, err := s.appCall(r, orgSlug, org.ActionInstallationCreate)
	if err != nil {
		writeError(w, err)
		return
	}
	if s.policyRelay == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "policy service is not configured"))
		return
	}
	var body apigen.InstallationCreation
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	a, err := visibleApp(r, stores, orgGRN(orgSlug), body.App)
	if err != nil {
		writeError(w, err)
		return
	}
	scope, err := s.installationScope(r, orgSlug, body.Projects)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := a.Requestable(stores.declared); err != nil {
		writeError(w, err)
		return
	}
	installation, err := app.NewInstallation(orgGRN(orgSlug), a, u.GRN, scope)
	if err != nil {
		writeError(w, err)
		return
	}
	created, err := stores.records.CreateInstallation(r.Context(), installation)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.awaitMarks(r.Context(), orgGRN(orgSlug).String(),
		policyfeed.Entity{Type: policyfeed.InstallationType, ID: created.GRN.String()}); err != nil {
		writeError(w, err)
		return
	}
	if err := s.grantInstallation(r.Context(), stores, a, created); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, installationToAPI(created))
}

// grantInstallation writes and activates each `permit` an install
// grants, as the owner who installed, since writing one is
// `iam:permit.write`. It first purges what names the installation, so
// a reinstall inherits nothing even where an earlier uninstall's purge
// failed. A failure uninstalls what was written, the installation and
// its `permit`s, and answers the failure.
func (s *Server) grantInstallation(ctx context.Context, stores *appStores, a *app.App, installation *app.Installation) error {
	owner := installation.CreatedBy
	grants, err := installation.Grants(a, stores.declared)
	if err == nil {
		_, err = s.policyRelay.Purge(ctx, owner, installation.Organization, []grn.GRN{installation.GRN})
	}
	for i := 0; err == nil && i < len(grants); i++ {
		write := policyrelay.Write{Writer: owner, Org: installation.Organization, ID: grants[i].ID}
		err = s.policyRelay.Create(ctx, write, grants[i].Name, policyrelay.Body{Kind: policyrelay.KindPermit, Text: grants[i].Text})
		if err == nil {
			err = s.policyRelay.Activate(ctx, write)
		}
	}
	if err == nil {
		return nil
	}
	if _, undo := s.uninstall(ctx, stores, owner, installation); undo != nil {
		return errors.Join(err, errorx.Wrap(undo, "undo the installation", errorx.KindInternal))
	}
	return err
}

// uninstall deletes the installation with its instances, then every
// policy naming the installation or one of them, whoever wrote it, for
// actor (ADR-0192 decision 4), and answers the instances it removed.
// A purge that fails after the deletion leaves policies naming a GRN
// no record holds: an instance's is never minted again, and the next
// install of the app purges the installation's before it grants.
func (s *Server) uninstall(ctx context.Context, stores *appStores, actor grn.GRN, installation *app.Installation) ([]grn.GRN, error) {
	removed, err := stores.records.DeleteInstallation(ctx, installation.GRN)
	if err != nil {
		return nil, err
	}
	entities := append([]grn.GRN{installation.GRN}, removed...)
	if _, err := s.policyRelay.Purge(ctx, actor, installation.Organization, entities); err != nil {
		return nil, err
	}
	return removed, nil
}

// installationScope parses the projects an install names: none is the
// whole organization, and each named must be a project the organization
// holds.
func (s *Server) installationScope(r *http.Request, orgSlug string, projects *[]string) (app.Scope, error) {
	if projects == nil {
		return app.Scope{}, nil
	}
	grns := make([]grn.GRN, 0, len(*projects))
	for _, slug := range *projects {
		g := projectGRN(orgSlug, slug)
		if err := g.Validate(); err != nil {
			return app.Scope{}, err
		}
		if _, err := s.orgs.GetProject(r.Context(), g); err != nil {
			return app.Scope{}, err
		}
		grns = append(grns, g)
	}
	return app.ProjectScope(orgGRN(orgSlug), grns)
}

// ListInstallations answers the organization's installations.
func (s *Server) ListInstallations(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	_, stores, err := s.appCall(r, orgSlug, org.ActionInstallationRead)
	if err != nil {
		writeError(w, err)
		return
	}
	installations, err := stores.records.ListInstallations(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Installation, 0, len(installations))
	for _, i := range installations {
		out = append(out, installationToAPI(i))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetInstallation answers one of the organization's installations.
func (s *Server) GetInstallation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug) {
	_, _, installation, err := s.installationCall(r, orgSlug, installationSlug, org.ActionInstallationRead)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, installationToAPI(installation))
}

// DeleteInstallation uninstalls the app: the installation goes with its
// instances, their keys and their tokens, and so does every policy
// naming any of them (ADR-0192 decision 4).
func (s *Server) DeleteInstallation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug) {
	u, stores, installation, err := s.installationCall(r, orgSlug, installationSlug, org.ActionInstallationDelete)
	if err != nil {
		writeError(w, err)
		return
	}
	if s.policyRelay == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "policy service is not configured"))
		return
	}
	// The deletion takes the installation's instances, and each of
	// those marks its instance, so the answer waits for those it took.
	removed, err := s.uninstall(r.Context(), stores, u.GRN, installation)
	if err != nil {
		writeError(w, err)
		return
	}
	marks := []policyfeed.Entity{{Type: policyfeed.InstallationType, ID: installation.GRN.String()}}
	for _, g := range removed {
		marks = append(marks, policyfeed.Entity{Type: policyfeed.InstanceType, ID: g.String()})
	}
	if err := s.awaitMarks(r.Context(), orgGRN(orgSlug).String(), marks...); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func appToAPI(a *app.App) apigen.App {
	return apigen.App{
		Grn:                a.GRN.String(),
		Slug:               a.Slug,
		Name:               a.Name,
		Description:        a.Description,
		Visibility:         apigen.AppVisibility(a.Visibility),
		RequestedActions:   append([]string{}, a.RequestedActions...),
		RequestsBotHosting: a.RequestsBotHosting,
		Instances:          apigen.AppInstances(a.Instances),
		Delivery:           apigen.AppDelivery(a.Delivery),
	}
}

func installationToAPI(i *app.Installation) apigen.Installation {
	projects := make([]string, 0, len(i.Scope.Projects()))
	for _, p := range i.Scope.Projects() {
		projects = append(projects, p.String())
	}
	return apigen.Installation{
		Grn:         i.GRN.String(),
		OrgGrn:      i.Organization.String(),
		AppGrn:      i.App.String(),
		Incarnation: i.Incarnation,
		Projects:    projects,
		CreatedBy:   i.CreatedBy.String(),
		CreatedAt:   i.CreatedAt,
	}
}
