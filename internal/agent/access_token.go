package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// AccessTokenPrefix is what an agent token carries, so a scanner
// recognises one and a reader can tell it from an operator's
// (ADR-0100).
const AccessTokenPrefix = "garam_at_"

// The agent token's lifetime bounds (ADR-0100). The lower bound is set
// for the refresh margin: an assignee mints the next token between two
// thirds and three quarters of the current one's life, and a quarter
// of twenty minutes is the five a Secret's propagation has to fit in.
const (
	DefaultAccessTokenLifetime = time.Hour
	MinAccessTokenLifetime     = 20 * time.Minute
	MaxAccessTokenLifetime     = 24 * time.Hour
)

// LiveAccessTokensPerAudience is how many live tokens an agent holds
// for one audience, and LiveAccessTokensPerAgent how many it holds in
// all: a mint beyond either drops the oldest, which bounds what an
// assignee's mints can write however many audiences it names
// (ADR-0100). An assignee refreshes a garam token and a control
// service's, so the total is two audiences' worth.
const (
	LiveAccessTokensPerAudience = 5
	LiveAccessTokensPerAgent    = 2 * LiveAccessTokensPerAudience
)

// accessTokenBytes sizes the secret: 256 bits (ADR-0100).
const accessTokenBytes = 32

// Sentinels of the agent token.
var (
	// ErrAccessTokenInvalid refuses an agent token no live row stands
	// for: unknown, expired, minted at an earlier epoch or lineage, or
	// withdrawn with its agent.
	ErrAccessTokenInvalid = errorx.New(errorx.KindUnauthenticated, "agent token is not valid")
	// ErrAccessTokenLifetime refuses a configured lifetime outside the
	// bounds above.
	ErrAccessTokenLifetime = errorx.New(errorx.KindInvalidArgument,
		"an agent token's lifetime is at least 20m and at most 24h")
	// ErrAccessTokenAudience refuses an audience that is not another
	// operator of the agent's organization.
	ErrAccessTokenAudience = errorx.New(errorx.KindInvalidArgument,
		"an agent token's audience is another operator of the agent's organization")
	// ErrNoCertificate refuses a caller that presented no certificate
	// to a route that renews one: a token's holder is minted another
	// token instead (ADR-0100).
	ErrNoCertificate = errorx.New(errorx.KindUnauthenticated,
		"no certificate was presented: a token is not renewed, its assignee mints another")
)

// AccessToken is a short-lived token an agent's assignee minted for it
// (ADR-0100). It exists in this value alone; what is stored is its hash.
//
// Epoch and Lineage are what the agent's assignment and execution held
// when it was minted, which every request presenting it is read
// against: a reassignment or a recovery refuses it from the next
// request.
type AccessToken struct {
	Value    string
	Agent    grn.GRN
	Assignee grn.GRN
	Epoch    int
	Lineage  *Lineage
	// Audience is the hosted operator the token is accepted by, or nil
	// where it is accepted by garam itself.
	Audience  *grn.GRN
	IssuedAt  time.Time
	ExpiresAt time.Time
	// ServerRootPEM is the garam server root the holder pins garam by.
	ServerRootPEM []byte
}

// Hash returns the persisted form of the token.
func (t *AccessToken) Hash() string { return hashAccessToken(t.Value) }

// Serial returns what the execution fence is handed for this token in
// place of a certificate's serial: `t` and the first half of its hash,
// which no decimal certificate serial can equal.
func (t *AccessToken) Serial() string { return accessTokenSerial(t.Hash()) }

// AccessTokenHolder is what a live agent token's row names, read
// against the agent's assignment and execution as they stand now.
type AccessTokenHolder struct {
	Agent     grn.GRN
	Serial    string
	Audience  *grn.GRN
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// HashAccessToken derives the persisted form of a presented agent token.
func HashAccessToken(token string) string { return hashAccessToken(token) }

// WellFormedAccessToken reports whether value is the prefix and 256
// bits of lowercase hex. A value that is not is refused before anything
// is read for it.
func WellFormedAccessToken(value string) bool { return wellFormedSecret(value, AccessTokenPrefix) }

// wellFormedSecret reports whether value is prefix and 256 bits of
// lowercase hex, the shape of every secret this package issues.
func wellFormedSecret(value, prefix string) bool {
	rest, ok := strings.CutPrefix(value, prefix)
	if !ok || len(rest) != 2*accessTokenBytes {
		return false
	}
	for _, c := range rest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ParseAccessTokenLifetime bounds a configured lifetime; zero is the
// default.
func ParseAccessTokenLifetime(lifetime time.Duration) (time.Duration, error) {
	if lifetime == 0 {
		return DefaultAccessTokenLifetime, nil
	}
	if lifetime < MinAccessTokenLifetime || lifetime > MaxAccessTokenLifetime {
		return 0, ErrAccessTokenLifetime
	}
	return lifetime, nil
}

func hashAccessToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func accessTokenSerial(hash string) string { return "t" + hash[:32] }

// newAccessTokenValue draws 256 bits behind the prefix.
func newAccessTokenValue() (string, error) { return newSecret(AccessTokenPrefix) }

// newSecret draws 256 bits behind prefix.
func newSecret(prefix string) (string, error) {
	var b [accessTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, prefix+" secret")
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
