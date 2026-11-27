package api

import (
	"net/http"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/org"
)

// GetInstanceReport answers the instance's latest report and its latest
// contact (ADR-0188). What the instance said is answered as data and
// never read here (REP-9).
func (s *Server) GetInstanceReport(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceRead)
	if err != nil {
		writeError(w, err)
		return
	}
	if stores.reports == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "reports are not configured"))
		return
	}
	kept, err := stores.reports.LatestReport(r.Context(), i.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	contact, err := stores.records.LastContact(r.Context(), i.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reportToAPI(kept, contact))
}

// reportToAPI is kept with the instance's last contact, which the app
// system records.
func reportToAPI(kept bot.KeptReport, contact *time.Time) apigen.InstanceReportView {
	view := apigen.InstanceReportView{Instance: kept.Instance.String(), LastContactAt: contact}
	if kept.Report == nil {
		return view
	}
	r := kept.Report
	out := &apigen.InstanceReport{
		ObservedAt: r.ObservedAt,
		Connector: apigen.ReportConnector{
			Version: r.Connector.Version, Mode: apigen.ReportConnectorMode(r.Connector.Mode),
			Namespaces: r.Connector.Namespaces,
		},
		Declarations: make([]apigen.DeclarationOutcome, 0, len(r.Declarations)),
		Objects:      make([]apigen.ReportedObject, 0, len(r.Objects)),
	}
	if r.Connector.ClaimedElsewhere != nil {
		out.Connector.ClaimedElsewhere = &r.Connector.ClaimedElsewhere
	}
	for _, o := range r.Declarations {
		out.Declarations = append(out.Declarations, apigen.DeclarationOutcome{
			Id: o.ID, Generation: o.Generation, Outcome: apigen.DeclarationOutcomeOutcome(o.Outcome),
			Message: optional(o.Message),
		})
	}
	for _, o := range r.Objects {
		object := apigen.ReportedObject{
			ApiVersion: o.APIVersion, Kind: apigen.ReportedObjectKind(o.Kind), Namespace: o.Namespace,
			Name: o.Name, Uid: o.UID, Owner: apigen.ReportedObjectOwner(o.Owner), Generation: o.Generation,
			Cluster: optional(o.Cluster), DeclarationId: optional(o.DeclarationID), Orphaned: o.Orphaned,
			Status: o.Status,
		}
		if o.Spec != nil {
			spec := o.Spec
			object.Spec = &spec
		}
		out.Objects = append(out.Objects, object)
	}
	view.Report = out
	return view
}

// optional is v as an optional member: absent where it is zero.
func optional[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}
