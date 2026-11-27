package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/operator"
)

// ReportMaxBytes is REP-6's bound on a report's body: past it the
// connection answers 413 report_too_large.
const ReportMaxBytes = 4 << 20

// ReportsPerInstance bounds the reports one instance sends per
// operator.ExchangeWindow (ADR-0188 decision 5): two a second, far past
// REP-4's pace of one within 10 seconds of a change.
const ReportsPerInstance = 120

// Report is cluster-connection's Report as garam keeps it: the members
// the interface names and no others (T-4). Every spec, status and
// message is display data, kept as it came and never read (REP-9).
type Report struct {
	ObservedAt   time.Time            `json:"observedAt"`
	Connector    ReportConnector      `json:"connector"`
	Declarations []DeclarationOutcome `json:"declarations"`
	Objects      []ReportedObject     `json:"objects"`
}

// ReportConnector is a report's connector member (REP-1).
type ReportConnector struct {
	Version          string   `json:"version"`
	Mode             string   `json:"mode"`
	Namespaces       []string `json:"namespaces"`
	ClaimedElsewhere []string `json:"claimedElsewhere,omitempty"`
}

// DeclarationOutcome is what the connector made of one declaration.
type DeclarationOutcome struct {
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
	Outcome    string `json:"outcome"`
	Message    string `json:"message,omitempty"`
}

// ReportedObject is one object the connector reports (REP-2).
type ReportedObject struct {
	APIVersion    string          `json:"apiVersion"`
	Kind          string          `json:"kind"`
	Namespace     string          `json:"namespace"`
	Name          string          `json:"name"`
	UID           string          `json:"uid"`
	Owner         string          `json:"owner"`
	Generation    int64           `json:"generation"`
	Cluster       string          `json:"cluster,omitempty"`
	DeclarationID string          `json:"declarationId,omitempty"`
	Orphaned      *bool           `json:"orphaned,omitempty"`
	Spec          json.RawMessage `json:"spec,omitempty"`
	Status        json.RawMessage `json:"status"`
}

// The owners an object is reported under.
const (
	OwnerGaram   = "garam"
	OwnerCluster = "cluster"
)

var (
	declaredKinds  = []string{"Bot", "Workspace", "WorkspaceClass", "BotPool"}
	connectorModes = []string{"apply", "reportOnly"}
	outcomes       = []string{"applied", "conflict", "invalid", "namespaceNotWatched"}
	owners         = []string{OwnerGaram, OwnerCluster}
)

// ParseReport reads raw as a report, refusing one that misses a
// member the interface requires or carries an object of a kind that is
// not declared, a Secret and a ConfigMap among them (REP-7).
func ParseReport(raw []byte) (Report, error) {
	var r Report
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&r); err != nil {
		return Report{}, invalidReport("the body is not a report")
	}
	switch {
	case r.ObservedAt.IsZero():
		return Report{}, invalidReport("observedAt is missing")
	case r.Connector.Version == "" || !slices.Contains(connectorModes, r.Connector.Mode):
		return Report{}, invalidReport("connector.version or connector.mode is missing or unknown")
	case r.Connector.Namespaces == nil || r.Declarations == nil || r.Objects == nil:
		return Report{}, invalidReport("connector.namespaces, declarations or objects is missing")
	}
	for _, o := range r.Declarations {
		if o.ID == "" || o.Generation < 1 || !slices.Contains(outcomes, o.Outcome) {
			return Report{}, invalidReport("a declaration outcome is incomplete or unknown")
		}
	}
	for _, o := range r.Objects {
		if !slices.Contains(declaredKinds, o.Kind) {
			return Report{}, invalidReport("REP-7: an object is not of a declared kind")
		}
		if !slices.Contains(owners, o.Owner) || o.Generation < 1 || o.Status == nil {
			return Report{}, invalidReport("an object lacks its owner, generation or status")
		}
	}
	return r, nil
}

func invalidReport(message string) error {
	return errorx.New(errorx.KindInvalidArgument, message)
}

// identifiers are the declaration identifiers r names.
func (r Report) identifiers() []string {
	ids := make([]string, 0, len(r.Declarations)+len(r.Objects))
	for _, o := range r.Declarations {
		ids = append(ids, o.ID)
	}
	for _, o := range r.Objects {
		if o.DeclarationID != "" {
			ids = append(ids, o.DeclarationID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// placedOn is r without the outcomes and objects naming a declaration
// not placed on g (REP-8), and without a garam-owned object whose
// marker names another record or no declaration.
func (r Report) placedOn(g grn.GRN, placed map[string]bool) Report {
	r.Declarations = slices.DeleteFunc(slices.Clone(r.Declarations), func(o DeclarationOutcome) bool {
		return !placed[o.ID]
	})
	r.Objects = slices.DeleteFunc(slices.Clone(r.Objects), func(o ReportedObject) bool {
		if o.Owner != OwnerGaram {
			return o.DeclarationID != "" && !placed[o.DeclarationID]
		}
		return !placed[o.DeclarationID] || (o.Cluster != "" && o.Cluster != g.String())
	})
	return r
}

// KeptReport is what garam holds of an instance's reports: its latest
// report, nil where there is none. Its last contact is the app
// system's, on app.Instance.
type KeptReport struct {
	Instance grn.GRN
	Report   *Report
}

// Report keeps r as caller's report where it was observed after the one
// kept, with only what names a declaration placed on caller (REP-5,
// REP-8). A record deleted meanwhile is operator.ErrClientInvalid.
func (s *service) Report(ctx context.Context, caller grn.GRN, r Report) error {
	if !app.IsInstanceGRN(caller) {
		return operator.ErrClientInvalid
	}
	if err := s.reportsLimit.Check(caller.String()); err != nil {
		return err
	}
	placed, err := s.reports.PlacedDeclarationIDs(ctx, caller, r.identifiers())
	if err != nil {
		return err
	}
	err = s.reports.KeepReport(ctx, caller, r.placedOn(caller, placed))
	if errors.Is(err, app.ErrInstanceNotFound) {
		return operator.ErrClientInvalid
	}
	return err
}
