package operator

import (
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// AuthorityTTL is the longest a handed-over authority stays usable. It
// covers one browser-to-Operator call and its retries; a later attempt
// mints again under a fresh session, which rechecks everything.
const AuthorityTTL = 5 * time.Minute

// authorityTokenBytes sizes the opaque secret (#1160).
const authorityTokenBytes = 32

// BodyDigest is the lowercase hexadecimal SHA-256 of the exact request
// body the Operator receives. garam never sees that body; it binds the
// digest so the Operator can refuse a different one.
type BodyDigest string

var bodyDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseBodyDigest parses a body digest.
func ParseBodyDigest(s string) (BodyDigest, error) {
	if !bodyDigestPattern.MatchString(s) {
		return "", errorx.New(errorx.KindInvalidArgument, "body_sha256 must be 64 lowercase hexadecimal characters")
	}
	return BodyDigest(s), nil
}

// Assignment is where an agent runs: the controller it is assigned to
// and the assignment epoch, which garam treats as opaque here.
type Assignment struct {
	Operator grn.GRN
	Epoch    string
}

// OperationRef identifies one logical operation durably. It outlives
// every short token minted for it.
type OperationRef string

// Binding is everything an operation authority binds: who asked, which
// operator may act, on what, under which grant, and for which body. A
// configuration change also binds the placement it was authorized for.
type Binding struct {
	Ref        OperationRef
	Grant      GrantID
	Org        grn.GRN
	Actor      grn.GRN
	Audience   grn.GRN
	Operation  Operation
	Target     grn.GRN
	Assignment *Assignment
	RequestID  RequestID
	Digest     BodyDigest
}

// MintRequest is a member's request to hand an operator one operation.
type MintRequest struct {
	Actor     grn.GRN
	Org       grn.GRN
	Audience  grn.GRN
	Operation Operation
	Target    grn.GRN
	RequestID RequestID
	Digest    BodyDigest
}

// Minted is what a mint answers: the secret, once, and the durable
// reference and grant it is bound to.
type Minted struct {
	Authority string
	ExpiresAt time.Time
	Ref       OperationRef
	Grant     GrantID
}

// Introspected is a binding as introspection answers it, with the
// expiry of the token that was presented.
type Introspected struct {
	Binding
	ExpiresAt time.Time
}

// newAuthorityToken draws an opaque authority secret.
func newAuthorityToken() (string, error) {
	var b [authorityTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "operation authority")
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// Operation authority sentinels.
var (
	ErrOperationNotMintable = errorx.New(errorx.KindInvalidArgument, "only agent:create and agent:configure can be handed to an operator")
	// ErrAuthorityNotFound is one answer for an unknown, expired, or
	// another audience's authority, so a caller learns nothing about
	// authorities it does not hold.
	ErrAuthorityNotFound = errorx.New(errorx.KindNotFound, "operation authority not found")
	// ErrAuthorityRevoked refuses a known operation that current state
	// no longer authorizes: membership, role, policy, grant, a
	// registration, or the bound placement changed.
	ErrAuthorityRevoked = errorx.New(errorx.KindPermissionDenied, "the operation is no longer authorized")
	// ErrAuthorityRequestConflict refuses a request identifier already
	// bound to another operation, grant or placement. A newly
	// authorized operation takes a new request identifier.
	ErrAuthorityRequestConflict = errorx.New(errorx.KindAlreadyExists, "request_id is bound to a different operation")
	// ErrNotDelegated refuses a mint the hosted operator's current grant
	// does not cover.
	ErrNotDelegated = errorx.New(errorx.KindPermissionDenied, "the operator's delegation does not cover this operation")
)
