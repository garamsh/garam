package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/cluster"
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
	writeJSON(w, http.StatusOK, reportToAPI(kept))
}

func reportToAPI(kept cluster.KeptReport) apigen.ClusterReportView {
	view := apigen.ClusterReportView{Instance: kept.Cluster.String(), LastContactAt: kept.LastContactAt}
	if kept.Report == nil {
		return view
	}
	r := kept.Report
	out := &apigen.ClusterReport{
		ObservedAt: r.ObservedAt,
		Connector: apigen.ClusterReportConnector{
			Version: r.Connector.Version, Mode: apigen.ClusterReportConnectorMode(r.Connector.Mode),
			Namespaces: r.Connector.Namespaces,
		},
		Declarations: make([]apigen.ClusterDeclarationOutcome, 0, len(r.Declarations)),
		Objects:      make([]apigen.ClusterReportedObject, 0, len(r.Objects)),
	}
	if r.Connector.ClaimedElsewhere != nil {
		out.Connector.ClaimedElsewhere = &r.Connector.ClaimedElsewhere
	}
	for _, o := range r.Declarations {
		out.Declarations = append(out.Declarations, apigen.ClusterDeclarationOutcome{
			Id: o.ID, Generation: o.Generation, Outcome: apigen.ClusterDeclarationOutcomeOutcome(o.Outcome),
			Message: optional(o.Message),
		})
	}
	for _, o := range r.Objects {
		object := apigen.ClusterReportedObject{
			ApiVersion: o.APIVersion, Kind: apigen.ClusterReportedObjectKind(o.Kind), Namespace: o.Namespace,
			Name: o.Name, Uid: o.UID, Owner: apigen.ClusterReportedObjectOwner(o.Owner), Generation: o.Generation,
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
