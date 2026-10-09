package operator

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// The prefixes a credential and a token carry, so a scanner recognises
// one and a reader can tell which they hold (ADR-0100). The token's is
// the ADR's; the credential's follows the same rule, which the ADR
// states for tokens and leaves unnamed for the credential.
const (
	CredentialPrefix = "garam_oc_"
	TokenPrefix      = "garam_ot_"
)

// TokenContractVersion is the contract the exchange serves under
// (ADR-0100): the value a request carries and its answer echoes.
const TokenContractVersion = "machine-token.v1"

// CredentialScheme is the Authorization scheme an exchange presents the
// credential under (ADR-0100).
const CredentialScheme = "Garam-Credential"

// TokenTTL is how long an operator token is accepted. It is a constant
// rather than configuration (ADR-0100): the operator holds the token
// in memory and refreshes it in the last third of its life, so nothing
// a deployment does widens what a lifetime would buy it.
const TokenTTL = time.Hour

// secretBytes sizes a credential and a token: 256 bits each (ADR-0100).
const secretBytes = 32

// LiveTokensPerCredential is how many live tokens one credential
// issuance holds: an exchange beyond it drops the oldest (ADR-0100).
// An operator refreshes its token once an hour at most, so five covers
// every refresh it makes and bounds what a leaked credential can write.
const LiveTokensPerCredential = 5

// WellFormed reports whether value is a secret prefix issued: the
// prefix followed by 256 bits in lowercase hex. A value that is not is
// refused before anything is read for it.
func WellFormed(value, prefix string) bool {
	rest, ok := strings.CutPrefix(value, prefix)
	if !ok || len(rest) != 2*secretBytes {
		return false
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Sentinels of the credential and token half.
var (
	// ErrCredentialInvalid refuses a credential that authenticates no
	// operator. Unknown, voided by a re-issue and withdrawn with the
	// record are one refusal, for the reason [ErrEnrollmentInvalid]
	// gives: telling them apart answers questions about credentials
	// the caller does not hold.
	ErrCredentialInvalid = errorx.New(errorx.KindUnauthenticated, "operator credential is not valid")
	// ErrTokenInvalid refuses a token no live row stands for: unknown,
	// expired, or voided by a re-issue or a delete.
	ErrTokenInvalid = errorx.New(errorx.KindUnauthenticated, "operator token is not valid")
	// ErrUnsupportedTokenContract refuses an exchange carrying no
	// contract version, or another.
	ErrUnsupportedTokenContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+TokenContractVersion)
	// ErrNoCertificate refuses a renewal for a caller that presented
	// no certificate to renew: a token's holder exchanges its
	// credential instead (ADR-0100).
	ErrNoCertificate = errorx.New(errorx.KindUnauthenticated,
		"no certificate was presented: a token is not renewed, its credential is exchanged again")
	// ErrAudienceInvalid refuses an audience that is not another
	// operator of the exchanging operator's organization. A token
	// minted for a hosted operator is shown to it, so the audience is
	// a peer the organization registered and never the caller itself.
	ErrAudienceInvalid = errorx.New(errorx.KindInvalidArgument,
		"a token's audience is another operator of the same organization")
)

// IssuedCredential is a credential garam generated for an operator,
// carrying the value it answers once (ADR-0100).
//
// The value exists in this value alone. What reaches the database is
// [HashToken] of it, so a row read out of a dump authenticates nothing,
// and the caller that issued it is the only one ever shown it.
type IssuedCredential struct {
	value    string
	operator grn.GRN
	issuedBy grn.GRN
	issuedAt time.Time
}

// NewIssuedCredential draws a credential for the operator. issuedBy is
// the member registering or re-issuing.
func NewIssuedCredential(operatorGRN, issuedBy grn.GRN) (*IssuedCredential, error) {
	if operatorGRN.ServiceType != ServiceType {
		return nil, ErrNotAnOperator
	}
	value, err := secret(CredentialPrefix)
	if err != nil {
		return nil, errorx.Wrap(err, "operator credential")
	}
	return &IssuedCredential{value: value, operator: operatorGRN, issuedBy: issuedBy, issuedAt: time.Now().UTC()}, nil
}

// Value returns the credential the person places in the operator's
// cluster. It is answered once, by the call that issued it.
func (c *IssuedCredential) Value() string { return c.value }

// Operator returns the operator the credential authenticates.
func (c *IssuedCredential) Operator() grn.GRN { return c.operator }

// IssuedBy returns the member that issued it.
func (c *IssuedCredential) IssuedBy() grn.GRN { return c.issuedBy }

// IssuedAt returns when it was issued.
func (c *IssuedCredential) IssuedAt() time.Time { return c.issuedAt }

// Hash returns the persisted form of the credential.
func (c *IssuedCredential) Hash() string { return HashToken(c.value) }

// Token is a short-lived token an operator exchanged its credential
// for (ADR-0100). Like the credential it exists in this value alone,
// and what is stored is its hash.
type Token struct {
	Value    string
	Operator grn.GRN
	// Audience is the hosted operator the token is accepted by, or nil
	// where it is accepted by garam itself.
	Audience  *grn.GRN
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Hash returns the persisted form of the token.
func (t *Token) Hash() string { return HashToken(t.Value) }

// TokenHolder is what a live token's row names, read beside the
// registration it rests on.
type TokenHolder struct {
	Operator  grn.GRN
	Audience  *grn.GRN
	ExpiresAt time.Time
}

// CredentialHolder is the operator a presented credential belongs to,
// and which issuance of it the credential is: the identifier a token
// minted under it references, so a re-issue voids those tokens.
type CredentialHolder struct {
	ID       int64
	Operator grn.GRN
}

// secret draws 256 random bits and renders them behind prefix.
func secret(prefix string) (string, error) {
	var b [secretBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
