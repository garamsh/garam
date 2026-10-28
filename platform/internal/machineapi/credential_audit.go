package machineapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/garamsh/garam/platform/internal/audit"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
)

// CredentialAudit records what the listener decided about a machine
// credential (ADR-0114). internal/audit/postgres's CredentialStore
// implements it.
type CredentialAudit interface {
	RecordCredential(ctx context.Context, c audit.Credential) error
}

// credentialAuditTimeout bounds one write of an event, which the
// answer waits on.
const credentialAuditTimeout = 2 * time.Second

// WithCredentialAudit has the server record every credential it
// issues, renews or refuses through a, and answers the server. A server
// without one records nothing, which is what a test of another route
// constructs.
func (s *Server) WithCredentialAudit(a CredentialAudit) *Server {
	s.credentialAudit = a
	return s
}

// auditCredential records c after the outcome it describes was
// decided, before the answer is written. A write that fails is logged
// with the event's fields and the answer stands: the credential was
// already issued or refused, and refusing to answer it would strand a
// caller whose renewal does not replay (ADR-0114).
func (s *Server) auditCredential(r *http.Request, c audit.Credential) {
	if s.credentialAudit == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), credentialAuditTimeout)
	defer cancel()
	if err := s.credentialAudit.RecordCredential(ctx, c); err != nil {
		slog.ErrorContext(ctx, "credential event not recorded",
			slog.String("subject", c.Subject().String()), slog.String("credential", string(c.Kind())),
			slog.String("action", string(c.Action())), slog.String("outcome", string(c.Outcome())),
			slog.String("error", err.Error()))
	}
}

// refusedCredential records err as subject's refused request.
func (s *Server) refusedCredential(r *http.Request, subject grn.GRN, kind audit.CredentialKind, action audit.CredentialAction, err error) {
	s.auditCredential(r, audit.CredentialRefused(subject, kind, action, errorx.KindOf(err)))
}
