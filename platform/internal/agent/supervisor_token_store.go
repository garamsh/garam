package agent

import "context"

// SupervisorTokenStore answers the supervisor a presented credential
// belongs to, records the tokens it is exchanged for, and answers what
// a presented token stands for (ADR-0100). Every read joins the
// registration, so a revocation or a replacement refuses the credential
// and every token from the next request.
type SupervisorTokenStore interface {
	// SupervisorCredentialHolder answers the current registration
	// holding the credential credentialHash names, or
	// ErrSupervisorCredentialInvalid.
	SupervisorCredentialHolder(ctx context.Context, credentialHash string) (SupervisorCredentialHolder, error)
	// RecordSupervisorToken records t under the credential issuance
	// holder names and keeps that issuance's newest
	// LiveSupervisorTokensPerCredential, in one transaction holding the
	// issuance's row; an issuance re-issued meanwhile refuses with
	// ErrSupervisorCredentialInvalid.
	RecordSupervisorToken(ctx context.Context, holder SupervisorCredentialHolder, t *SupervisorToken) error
	// ResolveSupervisorToken answers what the live token tokenHash
	// names, or ErrSupervisorTokenInvalid.
	ResolveSupervisorToken(ctx context.Context, tokenHash string) (SupervisorTokenHolder, error)
}
