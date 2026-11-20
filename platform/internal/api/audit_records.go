package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/policyrelay"
)

// ListAuditRecords reads a page of the organization's audit records
// from the policy service as the signed-in user (ADR-0176). Whether
// the user may read them is the service's to decide on
// `iam:audit.read`, current, and it answers in the kind the response
// carries: the platform keeps no record and asks no question of its
// own.
func (s *Server) ListAuditRecords(
	w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListAuditRecordsParams,
) {
	pw, ok := s.relayed(w, r, orgSlug, "")
	if !ok {
		return
	}
	var size int32
	if params.PageSize != nil {
		size = *params.PageSize
	}
	var token string
	if params.PageToken != nil {
		token = *params.PageToken
	}
	page, err := s.policyRelay.ListAudit(r.Context(), pw.Writer, pw.Org, size, token)
	if err != nil {
		writeError(w, err)
		return
	}
	out := apigen.AuditRecordPage{Records: make([]apigen.AuditRecord, len(page.Records))}
	for i, rec := range page.Records {
		out.Records[i] = auditRecordToAPI(rec)
	}
	if page.NextPageToken != "" {
		out.NextPageToken = &page.NextPageToken
	}
	writeJSON(w, http.StatusOK, out)
}

func auditRecordToAPI(r policyrelay.AuditRecord) apigen.AuditRecord {
	out := apigen.AuditRecord{
		Id: r.ID, Kind: apigen.AuditRecordKind(r.Kind),
		FirstAt: r.FirstAt, LastAt: r.LastAt, Count: r.Count,
		Service: r.Service, Subject: r.Subject, Action: r.Action,
		Resource: r.Resource, Outcome: r.Outcome,
		PolicyIds: r.PolicyIDs, VersionFrom: r.VersionFrom, VersionTo: r.VersionTo,
	}
	if out.PolicyIds == nil {
		out.PolicyIds = []string{}
	}
	if !r.Day.IsZero() {
		out.Day = &r.Day
	}
	return out
}
