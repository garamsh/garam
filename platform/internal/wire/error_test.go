package wire_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/wire"
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
// TestMessageOf covers the discriminator that decides what a client
// reads: the chain's own text where this project wrote every part of
// it, and this project's sentence for the kind where the chain reaches
// an error from outside. The authored rows are the control — without
// them a MessageOf that answered the kind's sentence for everything
// would pass the rows that matter.
func TestMessageOf(t *testing.T) {
	driver := &pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23505",
		Message:        `duplicate key value violates unique constraint "users_pkey"`,
		ConstraintName: "users_pkey",
		TableName:      "users",
	}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"a sentinel this project wrote", errorx.New(errorx.KindNotFound, "invitation not found"), "invitation not found"},
		{"a chain of them", errorx.Wrap(errorx.New(errorx.KindNotFound, "user not found"), "get member"), "get member: user not found"},
		{"a driver error under a wrap", errorx.Wrap(driver, "create", errorx.KindAlreadyExists), "already exists"},
		{"a driver error under two", errorx.Wrap(errorx.Wrap(driver, "create", errorx.KindAlreadyExists), "register"), "already exists"},
		{"a decoder error under a wrap", errorx.Wrap(errors.New(`invalid character 'q'`), "request body", errorx.KindInvalidArgument), "the request is not in the required form"},
		{"an error with no kind at all", errors.New("bare"), "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, wire.MessageOf(tt.err))
		})
	}
	assert.Equal(t, "", wire.MessageOf(nil))

	// The property the disclosure turned on, asserted on the text
	// rather than on the equality above: nothing the driver named
	// survives.
	got := wire.MessageOf(errorx.Wrap(driver, "create", errorx.KindAlreadyExists))
	for _, leaked := range []string{"users_pkey", "users", "SQLSTATE", "23505", "duplicate key"} {
		assert.NotContains(t, got, leaked)
	}
}

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

// TestErrorOf reads a gRPC error back as an errorx error: the status
// code becomes the kind by name and the message is kept as written, so
// the service's own sentence reaches the caller. An error that carries
// no status reads as Unknown rather than as a kind it never named.
func TestErrorOf(t *testing.T) {
	for code, kind := range map[codes.Code]errorx.Kind{
		codes.InvalidArgument:  errorx.KindInvalidArgument,
		codes.PermissionDenied: errorx.KindPermissionDenied,
		codes.Unavailable:      errorx.KindUnavailable,
		codes.Internal:         errorx.KindInternal,
	} {
		got := wire.ErrorOf(status.Error(code, "the service's own sentence"))
		assert.True(t, errorx.IsKind(got, kind), "%v: got %v", code, got)
		assert.Equal(t, "the service's own sentence", got.Error())
	}
	got := wire.ErrorOf(errors.New("not a status"))
	assert.True(t, errorx.IsKind(got, errorx.KindUnknown), "got %v", got)
}

// TestErrorOf_ReadsTheReasonTheServiceSends marshals a status carrying
// an ErrorInfo to bytes and back, as the hop does, and reads the reason
// and its pointer out the other side. An ErrorInfo of another domain is
// the control: it reads with no reason.
func TestErrorOf_ReadsTheReasonTheServiceSends(t *testing.T) {
	cross := func(domain string) error {
		sent, err := status.New(codes.InvalidArgument, "Effect must be Allow or Deny").WithDetails(&errdetails.ErrorInfo{
			Reason:   "policy_document_effect_invalid",
			Domain:   domain,
			Metadata: map[string]string{"path": "/Statement/1/Effect"},
		})
		require.NoError(t, err)
		wireBytes, err := proto.Marshal(sent.Proto())
		require.NoError(t, err)
		var received spb.Status
		require.NoError(t, proto.Unmarshal(wireBytes, &received))
		return wire.ErrorOf(status.FromProto(&received).Err())
	}

	got := cross("garam.sh")
	assert.True(t, errorx.IsKind(got, errorx.KindInvalidArgument))
	assert.Equal(t, "Effect must be Allow or Deny", got.Error())
	reason, ok := errorx.ReasonOf(got)
	require.True(t, ok)
	assert.Equal(t, errorx.Reason{Code: "policy_document_effect_invalid", Path: "/Statement/1/Effect"}, reason)

	other := cross("example.com")
	_, ok = errorx.ReasonOf(other)
	assert.False(t, ok)
	assert.True(t, errorx.IsKind(other, errorx.KindInvalidArgument))
}
