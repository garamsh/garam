package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/audit"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// CredentialAudit records and reads machine credential events
// (ADR-0114). internal/audit/postgres's CredentialStore implements it.
type CredentialAudit interface {
	RecordCredential(ctx context.Context, c audit.Credential) error
	ListCredentials(ctx context.Context, subject grn.GRN, f listfilter.Filter) ([]audit.RecordedCredential, error)
}

// credentialAuditTimeout bounds one write of an event, which the
// answer waits on.
const credentialAuditTimeout = 2 * time.Second

// WithCredentialAudit has the server record the operator credentials
// it issues or refuses through a and answer listCredentialEvents from
// it, and answers the server. A server without one records nothing and
// answers that read unavailable.
func (s *Server) WithCredentialAudit(a CredentialAudit) *Server {
	s.credentialAudit = a
	return s
}

// auditCredential records c once the outcome it describes was decided,
// before the answer is written. A write that fails is logged and the
// answer stands, as on the machine listener (ADR-0114).
func (s *Server) auditCredential(r *http.Request, c audit.Credential) {
	if s.credentialAudit == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), credentialAuditTimeout)
	defer cancel()
	if err := s.credentialAudit.RecordCredential(ctx, c); err != nil {
		slog.ErrorContext(ctx, "credential event not recorded",
			slog.String("subject", c.Subject().String()), slog.String("credential", string(c.Kind())),
			slog.String("outcome", string(c.Outcome())), slog.String("error", err.Error()))
	}
}

// refusedCredential records err as the refusal of subject's credential.
func (s *Server) refusedCredential(r *http.Request, subject grn.GRN, kind audit.CredentialKind, err error) {
	s.auditCredential(r, audit.CredentialRefused(subject, kind, audit.ActionIssue, errorx.KindOf(err)))
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
		out.Reason = &reason
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
