package wire_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/wire"
)

// TestStatusOf covers every kind, so a kind that means something
// specific cannot answer 500 unnoticed; the rows are read against each
// other, so a mapping answering one status for everything fails all
// but one of them.
func TestStatusOf(t *testing.T) {
	tests := []struct {
		kind   errorx.Kind
		status int
	}{
		{errorx.KindUnknown, http.StatusInternalServerError},
		{errorx.KindInternal, http.StatusInternalServerError},
		{errorx.KindCanceled, http.StatusServiceUnavailable},
		{errorx.KindInvalidArgument, http.StatusBadRequest},
		{errorx.KindDeadlineExceeded, http.StatusGatewayTimeout},
		{errorx.KindNotFound, http.StatusNotFound},
		{errorx.KindAlreadyExists, http.StatusConflict},
		{errorx.KindPermissionDenied, http.StatusForbidden},
		{errorx.KindResourceExhausted, http.StatusTooManyRequests},
		{errorx.KindFailedPrecondition, http.StatusConflict},
		{errorx.KindAborted, http.StatusConflict},
		{errorx.KindOutOfRange, http.StatusBadRequest},
		{errorx.KindUnimplemented, http.StatusNotImplemented},
		{errorx.KindUnavailable, http.StatusServiceUnavailable},
		{errorx.KindDataLoss, http.StatusInternalServerError},
		{errorx.KindUnauthenticated, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.kind.String(), func(t *testing.T) {
			assert.Equal(t, tc.status, wire.StatusOf(tc.kind))
		})
	}
}

// TestKindOf covers every named code plus one no constant names, so
// the by-name mapping cannot silently lose a kind.
func TestKindOf(t *testing.T) {
	tests := []struct {
		code codes.Code
		kind errorx.Kind
	}{
		{codes.OK, errorx.KindUnknown},
		{codes.Canceled, errorx.KindCanceled},
		{codes.Unknown, errorx.KindUnknown},
		{codes.InvalidArgument, errorx.KindInvalidArgument},
		{codes.DeadlineExceeded, errorx.KindDeadlineExceeded},
		{codes.NotFound, errorx.KindNotFound},
		{codes.AlreadyExists, errorx.KindAlreadyExists},
		{codes.PermissionDenied, errorx.KindPermissionDenied},
		{codes.ResourceExhausted, errorx.KindResourceExhausted},
		{codes.FailedPrecondition, errorx.KindFailedPrecondition},
		{codes.Aborted, errorx.KindAborted},
		{codes.OutOfRange, errorx.KindOutOfRange},
		{codes.Unimplemented, errorx.KindUnimplemented},
		{codes.Internal, errorx.KindInternal},
		{codes.Unavailable, errorx.KindUnavailable},
		{codes.DataLoss, errorx.KindDataLoss},
		{codes.Unauthenticated, errorx.KindUnauthenticated},
		{codes.Code(99), errorx.KindUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.code.String(), func(t *testing.T) {
			assert.Equal(t, tc.kind, wire.KindOf(tc.code))
		})
	}
}
