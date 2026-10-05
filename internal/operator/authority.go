package operator

import (
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
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

// RequestTarget is the exact request target an Operator receives for
// a read or a publication: the escaped path and the raw query, with no
// scheme or authority. An empty body's digest is the same for every
// read, so the target is what tells one read from another; the
// Operator compares it, before decoding or routing, with the target it
// received. garam binds it and never decodes or reorders it.
type RequestTarget string

// maxRequestTarget bounds a request target in bytes.
const maxRequestTarget = 2048

// requestTargetPattern is RFC 9112's origin-form with no empty path
// segment: an absolute path of RFC 3986 segments and an optional query,
// every byte either an unreserved or delimiting character or a
// well-formed percent escape. Refusing an empty segment refuses a
// leading `//`, which some parsers read as an authority.
var requestTargetPattern = func() *regexp.Regexp {
	const pchar = `(?:[A-Za-z0-9\-._~!$&'()*+,;=:@]|%[0-9A-Fa-f]{2})`
	return regexp.MustCompile(`^(?:/` + pchar + `+)+(?:\?(?:` + pchar + `|[/?])*)?$`)
}()

// ParseRequestTarget parses an origin-form request target. It refuses
// a `.` or `..` path segment, which a server cleaning the path would
// remove, so the bound target never relies on the Operator not
// cleaning it.
func ParseRequestTarget(s string) (RequestTarget, error) {
	path, _, _ := strings.Cut(s, "?")
	if len(s) > maxRequestTarget || !requestTargetPattern.MatchString(s) || hasDotSegment(path) {
		return "", errorx.New(errorx.KindInvalidArgument,
			"request_target must be an origin-form path and query of at most 2048 bytes, with no dot segment")
	}
	return RequestTarget(s), nil
}

// Assignment is where an agent runs: what it is assigned to — a
// controller, or a standalone supervisor no grant covers — and the
// assignment epoch, which garam treats as opaque here.
type Assignment struct {
	Assignee grn.GRN
	Epoch    string
}

// OperationRef identifies one logical operation durably. It outlives
// every short token minted for it.
type OperationRef string

// Binding is everything an operation authority binds: who asked, which
// operator may act, on what, under which grant, and for which body. An
// operation on an existing agent also binds the placement it was
// authorized for, and a read or a publication the request target.
type Binding struct {
	Ref           OperationRef
	Grant         GrantID
	Org           grn.GRN
	Actor         grn.GRN
	Audience      grn.GRN
	Operation     Operation
	Target        grn.GRN
	Assignment    *Assignment
	RequestID     RequestID
	Digest        BodyDigest
	RequestTarget *RequestTarget
}

// MintRequest is a member's request to hand an operator one operation.
// RequestTarget is present exactly where the operation binds one.
type MintRequest struct {
	Actor         grn.GRN
	Org           grn.GRN
	Audience      grn.GRN
	Operation     Operation
	Target        grn.GRN
	RequestID     RequestID
	Digest        BodyDigest
	RequestTarget *RequestTarget
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
	ErrOperationNotMintable = errorx.New(errorx.KindInvalidArgument, "agent:activate cannot be handed to an operator")
	// ErrRequestTargetRequired refuses a read or a publication minted
	// without the request target it must bind.
	ErrRequestTargetRequired = errorx.New(errorx.KindInvalidArgument, "this operation binds a request_target")
	// ErrRequestTargetRefused refuses a request target on an operation
	// that binds only its body, whose authority stays as it was.
	ErrRequestTargetRefused = errorx.New(errorx.KindInvalidArgument, "this operation binds no request_target")
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
