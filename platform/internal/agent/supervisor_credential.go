package agent

import (
	"time"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
)

// The prefixes a supervisor's credential and tokens carry, so a
// scanner recognises one and a reader can tell it from an operator's
// or an agent's (ADR-0100).
const (
	SupervisorCredentialPrefix = "garam_sc_"
	SupervisorTokenPrefix      = "garam_st_"
)

// SupervisorTokenTTL is how long a supervisor token is accepted: an
// hour, a constant, as an operator's is (ADR-0100).
const SupervisorTokenTTL = time.Hour

// LiveSupervisorTokensPerCredential is how many live tokens one
// credential holds; an exchange beyond it drops the oldest, so a leaked
// credential cannot grow the table faster than its holder could use
// what it mints.
const LiveSupervisorTokensPerCredential = 5

// Sentinels of the supervisor's credential and tokens.
var (
	// ErrSupervisorCredentialInvalid refuses a credential no current
	// registration holds: unknown, re-issued by a retried spending, or
	// ended with its registration by a revocation or a replacement.
	ErrSupervisorCredentialInvalid = errorx.New(errorx.KindUnauthenticated, "supervisor credential is not valid")
	// ErrSupervisorTokenInvalid refuses a token no live row stands for.
	ErrSupervisorTokenInvalid = errorx.New(errorx.KindUnauthenticated, "supervisor token is not valid")
	// ErrSupervisorAudience refuses an exchange naming an audience: a
	// supervisor's token is garam's alone.
	ErrSupervisorAudience = errorx.New(errorx.KindInvalidArgument, "a supervisor's token names no audience")
	// ErrSupervisorMintAudience refuses a supervisor minting its
	// agent's token for an audience: it mints for garam alone, for the
	// one agent it holds (ADR-0100).
	ErrSupervisorMintAudience = errorx.New(errorx.KindInvalidArgument,
		"a supervisor mints its agent's tokens for garam alone")
)

// SupervisorCredential is the credential a standalone-enrollment.v2
// spending issues a supervisor. It exists in this value alone, and what
// is stored is its hash.
type SupervisorCredential struct {
	Value string
}

// NewSupervisorCredential draws a credential.
func NewSupervisorCredential() (*SupervisorCredential, error) {
	value, err := newSecret(SupervisorCredentialPrefix)
	if err != nil {
		return nil, err
	}
	return &SupervisorCredential{Value: value}, nil
}

// Hash returns the persisted form of the credential.
func (c *SupervisorCredential) Hash() string { return HashSupervisorSecret(c.Value) }

// SupervisorToken is an hour-long token a supervisor exchanged its
// credential for. Like the credential it exists in this value alone.
type SupervisorToken struct {
	Value      string
	Supervisor grn.GRN
	IssuedAt   time.Time
	ExpiresAt  time.Time
}

// Hash returns the persisted form of the token.
func (t *SupervisorToken) Hash() string { return HashSupervisorSecret(t.Value) }

// SupervisorCredentialHolder is the supervisor a presented credential
// belongs to, and which issuance of it the credential is: the
// identifier a token minted under it references, so a re-issue voids
// those tokens.
type SupervisorCredentialHolder struct {
	ID         int64
	Supervisor grn.GRN
}

// SupervisorTokenHolder is what a live supervisor token's row names,
// read beside the registration it rests on.
type SupervisorTokenHolder struct {
	Supervisor grn.GRN
	IssuedAt   time.Time
	ExpiresAt  time.Time
}

// HashSupervisorSecret derives the persisted form of a presented
// supervisor credential or token.
func HashSupervisorSecret(value string) string { return hashAccessToken(value) }

// WellFormedSupervisorSecret reports whether value is prefix and 256
// bits of lowercase hex. A value that is not is refused before anything
// is read for it.
func WellFormedSupervisorSecret(value, prefix string) bool { return wellFormedSecret(value, prefix) }
