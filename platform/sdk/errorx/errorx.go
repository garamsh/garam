// Package errorx is the single source of truth for typed errors.
//
// Kinds are gRPC-aligned (https://grpc.io/docs/guides/status-codes/).
// The wire-level mapping (HTTP status, gRPC code, JSON code) is owned
// by the transport layer, not by this package — errorx stays
// transport-agnostic so the same errors can flow over any surface.
//
// Usage:
//
//	return errorx.New(errorx.KindNotFound, "user not found")
//	return errorx.Wrap(err, "user lookup")
//	return errorx.Wrap(err, "user create", errorx.KindAlreadyExists)
//
// Inspect a chain with [IsKind] or [KindOf].
package errorx

import (
	"errors"
	"fmt"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindInternal
	KindCanceled
	KindInvalidArgument
	KindDeadlineExceeded
	KindNotFound
	KindAlreadyExists
	KindPermissionDenied
	KindResourceExhausted
	KindFailedPrecondition
	KindAborted
	KindOutOfRange
	KindUnimplemented
	KindUnavailable
	KindDataLoss
	KindUnauthenticated
)

func (k Kind) String() string {
	switch k {
	case KindCanceled:
		return "canceled"
	case KindInvalidArgument:
		return "invalid_argument"
	case KindDeadlineExceeded:
		return "deadline_exceeded"
	case KindNotFound:
		return "not_found"
	case KindAlreadyExists:
		return "already_exists"
	case KindPermissionDenied:
		return "permission_denied"
	case KindResourceExhausted:
		return "resource_exhausted"
	case KindFailedPrecondition:
		return "failed_precondition"
	case KindAborted:
		return "aborted"
	case KindOutOfRange:
		return "out_of_range"
	case KindUnimplemented:
		return "unimplemented"
	case KindInternal:
		return "internal"
	case KindUnavailable:
		return "unavailable"
	case KindDataLoss:
		return "data_loss"
	case KindUnauthenticated:
		return "unauthenticated"
	default:
		return "unknown"
	}
}

type Error interface {
	error
	Kind() Kind
	Unwrap() error
}

type errorx struct {
	kind    Kind
	message string
	cause   error
	reason  Reason
}

// Reason is a stable, documented code a client may branch on, where the
// kind alone does not say which refusal it was. Code names the rule
// that refused; Path, where the refusal is about one place in a
// document the caller sent, is a JSON Pointer (RFC 6901) to it. The
// zero Reason is none.
type Reason struct {
	Code string
	Path string
}

func (e *errorx) Error() string {
	if e.cause == nil {
		return e.message
	}
	return fmt.Sprintf("%s: %v", e.message, e.cause)
}

func (e *errorx) Kind() Kind { return e.kind }

func (e *errorx) Unwrap() error { return e.cause }

// New constructs an error with a fixed kind and message.
func New(kind Kind, message string) Error {
	return &errorx{kind: kind, message: message}
}

// NewWithReason constructs an error with a fixed kind, message and
// reason.
func NewWithReason(kind Kind, reason Reason, message string) Error {
	return &errorx{kind: kind, message: message, reason: reason}
}

// WrapWithReason is [Wrap] with an explicit kind and a reason, for a
// refusal whose cause is text this project did not write: the cause
// stays on the chain for the log, and the reason is what a client reads.
func WrapWithReason(err error, message string, kind Kind, reason Reason) Error {
	if err == nil {
		return nil
	}
	return &errorx{kind: kind, message: message, cause: err, reason: reason}
}

// ReasonOf returns the reason the first error in the chain carrying one
// was constructed with, and false where none does.
func ReasonOf(err error) (Reason, bool) {
	for err != nil {
		if e, ok := err.(*errorx); ok && e.reason.Code != "" {
			return e.reason, true
		}
		err = errors.Unwrap(err)
	}
	return Reason{}, false
}

// Wrap appends a context message to an error chain. Kind is taken
// from the chain unless explicitly overridden. Wrap(nil, ...) returns
// nil.
func Wrap(err error, message string, kind ...Kind) Error {
	if err == nil {
		return nil
	}
	k := KindOf(err)
	if len(kind) > 0 {
		k = kind[0]
	}
	return &errorx{kind: k, message: message, cause: err}
}

func KindOf(err error) Kind {
	for err != nil {
		if de, ok := err.(Error); ok {
			return de.Kind()
		}
		err = errors.Unwrap(err)
	}
	return KindUnknown
}

func IsKind(err error, k Kind) bool {
	return KindOf(err) == k
}
