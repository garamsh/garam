package operator

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// TokenPrefix is the prefix an operator token carries, so a scanner
// recognises one (ADR-0100).
const TokenPrefix = "garam_ot_"

// TokenContractVersion is the contract a supervisor's credential
// exchange and an agent token's mint serve under (ADR-0100): the value
// a request carries and its answer echoes. An operator exchanges a key
// under machine-token.v2 instead (ADR-0102, ADR-0117).
const TokenContractVersion = "machine-token.v1"

// CredentialScheme is the Authorization scheme a supervisor presents
// its credential under (ADR-0100).
const CredentialScheme = "Garam-Credential"

// TokenTTL is how long an operator token is accepted. It is a constant
// rather than configuration (ADR-0100): the operator holds the token
// in memory and refreshes it in the last third of its life, so nothing
// a deployment does widens what a lifetime would buy it.
const TokenTTL = time.Hour

// secretBytes sizes a token: 256 bits (ADR-0100).
const secretBytes = 32

// LiveTokensPerKey is how many live tokens one key holds: an
// exchange beyond it drops the oldest (ADR-0100, ADR-0102). An operator
// refreshes its token once an hour at most, so five covers every
// refresh it makes and bounds what a leaked key can write.
const LiveTokensPerKey = 5

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

// Sentinels of the token half.
var (
	// ErrTokenInvalid refuses a token no live row stands for: unknown,
	// expired, or voided by a key's withdrawal or a delete.
	ErrTokenInvalid = errorx.New(errorx.KindUnauthenticated, "operator token is not valid")
	// ErrUnsupportedTokenContract refuses an exchange carrying no
	// contract version, or another.
	ErrUnsupportedTokenContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+TokenContractVersion)
	// ErrAudienceInvalid refuses an audience that is not another
	// operator of the exchanging operator's organization. A token
	// minted for a hosted operator is shown to it, so the audience is
	// a peer the organization registered and never the caller itself.
	ErrAudienceInvalid = errorx.New(errorx.KindInvalidArgument,
		"a token's audience is another operator the organization registered")
	// ErrTokenNotProven refuses a token presented on another
	// operator's behalf that does not prove it: not a token's shape,
	// not live, naming another operator, or minted to be shown to
	// another audience (ADR-0100).
	ErrTokenNotProven = errorx.New(errorx.KindInvalidArgument,
		"the token does not prove the operator it is presented for")
)

// Token is a short-lived token an operator exchanged its key for
// (ADR-0102). It exists in this value alone, and what is stored is its
// hash.
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

// secret draws 256 random bits and renders them behind prefix.
func secret(prefix string) (string, error) {
	var b [secretBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// HashToken derives the persisted form of a token. Only the hash
// reaches the database, as it does for a session and for the state a
// login issues.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
