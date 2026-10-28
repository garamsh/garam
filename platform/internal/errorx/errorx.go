// Package errorx forwards to lib/errorx, where the implementation
// lives since a second module, cowork, needs it (ADR-0125, ADR-0132).
// It keeps this module's imports unchanged until #1659 rewrites them to
// the new path, and is deleted then; nothing new imports it.
package errorx

import "github.com/garamsh/garam/lib/errorx"

// Kind is [errorx.Kind].
type Kind = errorx.Kind

// Error is [errorx.Error].
type Error = errorx.Error

// Reason is [errorx.Reason].
type Reason = errorx.Reason

// The kinds, as [errorx] declares them.
const (
	KindUnknown            = errorx.KindUnknown
	KindInternal           = errorx.KindInternal
	KindCanceled           = errorx.KindCanceled
	KindInvalidArgument    = errorx.KindInvalidArgument
	KindDeadlineExceeded   = errorx.KindDeadlineExceeded
	KindNotFound           = errorx.KindNotFound
	KindAlreadyExists      = errorx.KindAlreadyExists
	KindPermissionDenied   = errorx.KindPermissionDenied
	KindResourceExhausted  = errorx.KindResourceExhausted
	KindFailedPrecondition = errorx.KindFailedPrecondition
	KindAborted            = errorx.KindAborted
	KindOutOfRange         = errorx.KindOutOfRange
	KindUnimplemented      = errorx.KindUnimplemented
	KindUnavailable        = errorx.KindUnavailable
	KindDataLoss           = errorx.KindDataLoss
	KindUnauthenticated    = errorx.KindUnauthenticated
)

// The constructors and readers, as [errorx] declares them.
var (
	New            = errorx.New
	NewWithReason  = errorx.NewWithReason
	WrapWithReason = errorx.WrapWithReason
	ReasonOf       = errorx.ReasonOf
	Wrap           = errorx.Wrap
	KindOf         = errorx.KindOf
	IsKind         = errorx.IsKind
)
