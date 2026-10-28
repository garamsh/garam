package api

import (
	"context"
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/audit"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// CredentialAudit reads the machine credential events the machine
// listener recorded (ADR-0114). internal/audit/postgres's
// CredentialStore implements it. The browser-facing listener issues no
// machine credential, so it records none (ADR-0117).
type CredentialAudit interface {
	ListCredentials(ctx context.Context, subject grn.GRN, f listfilter.Filter) ([]audit.RecordedCredential, error)
}

// WithCredentialAudit has the server answer listCredentialEvents from
// a, and answers the server. A server without one answers that read
// unavailable.
func (s *Server) WithCredentialAudit(a CredentialAudit) *Server {
	s.credentialAudit = a
	return s
}

// ListCredentialEvents answers the credential events recorded for one
// operator, agent or supervisor of the org, newest first. It is
// authorized on the subject's own read (ADR-0114).
func (s *Server) ListCredentialEvents(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListCredentialEventsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	subject, err := grn.Parse(params.Subject)
	if err != nil {
		writeError(w, errorx.Wrap(err, "subject", errorx.KindInvalidArgument))
		return
	}
	if subject.Organization != orgSlug {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "subject is not of this organization"))
		return
	}
	if err := s.authorizeSubjectRead(r, orgSlug, subject, u); err != nil {
		writeError(w, err)
		return
	}
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	if s.credentialAudit == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "credential events are not recorded by this server"))
		return
	}
	events, err := s.credentialAudit.ListCredentials(r.Context(), subject, f)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.CredentialEvent, 0, len(events))
	for _, e := range events {
		out = append(out, credentialEventToAPI(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// authorizeSubjectRead is the read each kind of subject already has:
// an operator's on the org, an agent's on the agent, and a
// supervisor's, which holds an agent, on the org.
func (s *Server) authorizeSubjectRead(r *http.Request, orgSlug string, subject grn.GRN, u *user.User) error {
	switch subject.ServiceType {
	case operator.ServiceType:
		return s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorRead)
	case agent.ServiceType:
		return s.authorizeOn(r, orgGRN(orgSlug), subject, u, org.ActionAgentRead)
	case agent.ServiceTypeSupervisor:
		return s.authorize(r, orgGRN(orgSlug), u, org.ActionAgentRead)
	}
	return errorx.New(errorx.KindInvalidArgument, "subject is an operator, an agent or a supervisor")
}

func credentialEventToAPI(e audit.RecordedCredential) apigen.CredentialEvent {
	out := apigen.CredentialEvent{
		SubjectGrn: e.Subject().String(),
		Credential: apigen.CredentialEventCredential(e.Kind()),
		Action:     apigen.CredentialEventAction(e.Action()),
		Outcome:    apigen.CredentialEventOutcome(e.Outcome()),
		Replay:     e.Replay(),
		RecordedAt: e.RecordedAt.UTC(),
	}
	if reason, ok := e.Reason(); ok {
		out.RefusalKind = &reason
	}
	if serial := e.Serial(); serial != "" {
		out.Serial = &serial
	}
	v := e.Validity()
	if !v.NotBefore.IsZero() {
		at := v.NotBefore.UTC()
		out.NotBefore = &at
	}
	if !v.NotAfter.IsZero() {
		at := v.NotAfter.UTC()
		out.NotAfter = &at
	}
	return out
}
