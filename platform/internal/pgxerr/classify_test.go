package pgxerr_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/pgxerr"
)

// sentinelNotFound is a placeholder notFound error used across tests.
var sentinelNotFound = errorx.New(errorx.KindNotFound, "sentinel not found")

// sentinelConflict is a placeholder conflict error used across tests.
var sentinelConflict = errorx.New(errorx.KindAlreadyExists, "sentinel conflict")

// pgError constructs a *pgconn.PgError with the given sqlstate code
// and a fixed message, the shape the driver returns for a constraint
// violation or a trigger exception. Severity is set so the standard
// format the driver emits — "ERROR: <message> (SQLSTATE <code>)" —
// is reproduced here.
func pgError(code string) *pgconn.PgError {
	return &pgconn.PgError{Code: code, Message: "constraint " + code, Severity: "ERROR"}
}

func TestClassify_NilReturnsNil(t *testing.T) {
	assert.Nil(t, pgxerr.Classify(nil, "create", sentinelNotFound, sentinelConflict))
}

// TestClassify_NoRowsUsesNotFound pins disagreement B (ErrNoRows
// handling) to the unified behaviour. The alternative in place before
// was ca's hardcoded "always return ErrAuthorityNotFound" — that
// ignores the caller-supplied sentinel. With ca's behaviour, the
// caller-supplied sentinel here would be replaced by the hardcoded one
// (different message, same KindNotFound), and an assertion on the
// message would fail.
func TestClassify_NoRowsUsesNotFound(t *testing.T) {
	got := pgxerr.Classify(pgx.ErrNoRows, "get", sentinelNotFound, nil)
	assert.Same(t, sentinelNotFound, got,
		"notFound is returned verbatim on ErrNoRows, so the caller's sentinel survives")
}

// TestClassify_NoRowsWithoutNotFound pins the other half of
// disagreement B: when notFound is nil the classifier falls through to
// a generic wrap with the operation name. The alternative in place
// before (ca's hardcode) would return ErrAuthorityNotFound
// unconditionally — a different error entirely. An assertion on the
// chain kind would pass either way (KindNotFound both sides), but the
// message and the wrap chain would not.
func TestClassify_NoRowsWithoutNotFound(t *testing.T) {
	got := pgxerr.Classify(pgx.ErrNoRows, "get", nil, nil)
	require.Error(t, got)
	assert.True(t, errorx.IsKind(got, errorx.KindUnknown),
		"no sentinel and no override: the chain's kind survives (KindUnknown here)")
	assert.Equal(t, "get: no rows in result set", got.Error())
}

// TestClassify_UniqueViolationUsesConflict pins disagreement C: every
// copy agreed on 23505, but the form of the agreement is the assertion
// here. With a caller-supplied conflict the conflict is returned
// verbatim; without it the classifier applies KindAlreadyExists. The
// alternative in place before (no caller-supplied sentinel ever on
// user, or always passing nil on ca) would wrap every 23505 with
// KindAlreadyExists — and the test below catches that.
func TestClassify_UniqueViolationUsesConflict(t *testing.T) {
	withConflict := pgxerr.Classify(pgError("23505"), "create", nil, sentinelConflict)
	assert.Same(t, sentinelConflict, withConflict,
		"23505 with a conflict: the caller's sentinel survives")

	withoutConflict := pgxerr.Classify(pgError("23505"), "create", nil, nil)
	require.Error(t, withoutConflict)
	assert.True(t, errorx.IsKind(withoutConflict, errorx.KindAlreadyExists),
		"23505 without a conflict: KindAlreadyExists")
	assert.Equal(t, "create: ERROR: constraint 23505 (SQLSTATE 23505)", withoutConflict.Error())
}

// TestClassify_ForeignKeyUsesNotFound pins disagreement D. The
// alternative in place before was org/ca's hardcoded "referenced row
// missing" — that ignored the caller-supplied notFound sentinel. With
// the unified behaviour, the notFound sentinel survives.
func TestClassify_ForeignKeyUsesNotFound(t *testing.T) {
	got := pgxerr.Classify(pgError("23503"), "attach policy", sentinelNotFound, nil)
	assert.Same(t, sentinelNotFound, got,
		"23503 with a notFound: the caller's sentinel survives")
}

// TestClassify_ForeignKeyWithoutNotFound is the other half of
// disagreement D: without a sentinel the classifier wraps with
// "<op>: referenced row missing" and KindNotFound. The alternative
// (org/ca) returned the same wrap, so this test is not a disagreement
// — it pins the message shape so a future change to it is loud.
func TestClassify_ForeignKeyWithoutNotFound(t *testing.T) {
	got := pgxerr.Classify(pgError("23503"), "attach policy", nil, nil)
	require.Error(t, got)
	assert.True(t, errorx.IsKind(got, errorx.KindNotFound))
	assert.Equal(t, "attach policy: referenced row missing: ERROR: constraint 23503 (SQLSTATE 23503)", got.Error())
}

// TestClassify_CheckViolationIsInvalidArgument pins disagreement E.
// The alternative in place before was org/ca/user: 23514 was not
// handled, so it fell through to the generic wrap with the chain's
// kind — which for a non-errorx driver error is KindUnknown. With the
// unified behaviour, every domain that hits 23514 gets
// KindInvalidArgument: a value outside the column's check is a caller
// mistake, not an outage. This is the behaviour agent alone had; the
// unification makes it the default.
func TestClassify_CheckViolationIsInvalidArgument(t *testing.T) {
	got := pgxerr.Classify(pgError("23514"), "report state", nil, nil)
	require.Error(t, got)
	assert.True(t, errorx.IsKind(got, errorx.KindInvalidArgument),
		"23514 check_violation is a caller mistake, not an outage")
	assert.Equal(t, "report state: ERROR: constraint 23514 (SQLSTATE 23514)", got.Error())
}

// TestClassify_RaiseExceptionIsFailedPrecondition is the other half of
// disagreement E: P0001 from a user-defined trigger is a system-state
// refusal, and the alternative in place before (org/ca/user, agent)
// did not handle it. With the unified behaviour every domain that
// hits P0001 gets KindFailedPrecondition. This is the behaviour authz
// alone had; the unification makes it the default.
func TestClassify_RaiseExceptionIsFailedPrecondition(t *testing.T) {
	got := pgxerr.Classify(pgError("P0001"), "set policy status", nil, nil)
	require.Error(t, got)
	assert.True(t, errorx.IsKind(got, errorx.KindFailedPrecondition),
		"P0001 raise_exception is a system-state refusal, not a transient error")
	assert.Equal(t, "set policy status: ERROR: constraint P0001 (SQLSTATE P0001)", got.Error())
}

// TestClassify_UnrecognisedCodePassesThrough pins the fallthrough:
// codes the classifier does not name wrap with op and the chain's
// kind. A driver error carries KindUnknown from the chain, so the
// outcome is a wrapped error with KindUnknown. The alternative in
// place before — and what would happen if 23514/P0001 were dropped
// from this function — is exactly the same wrap, so this test pins
// the wrap message to op alone (no store prefix) and the chain's kind.
func TestClassify_UnrecognisedCodePassesThrough(t *testing.T) {
	got := pgxerr.Classify(pgError("42P01"), "create org", nil, nil)
	require.Error(t, got)
	assert.True(t, errorx.IsKind(got, errorx.KindUnknown))
	assert.Equal(t, "create org: ERROR: constraint 42P01 (SQLSTATE 42P01)", got.Error())
	assert.NotContains(t, got.Error(), "store:",
		"the wrap message is op alone, not prefixed with the store name")
}

// TestClassify_WrapMessageIsOp pins disagreement G (wrap message):
// the unified classifier wraps with op, not with "<store> store: <op>".
// The alternative was every copy's "<store> store: <op>" prefix, which
// duplicated what the caller already knew. The assertion is on the
// absence of the prefix.
func TestClassify_WrapMessageIsOp(t *testing.T) {
	got := pgxerr.Classify(pgError("42P01"), "create org", nil, nil)
	require.Error(t, got)
	assert.Equal(t, "create org: ERROR: constraint 42P01 (SQLSTATE 42P01)", got.Error())
	assert.NotContains(t, got.Error(), "store:")
}

// TestClassify_NotFoundFallsThroughWhenNotPgError covers a non-pg
// error that happens not to be ErrNoRows: it is wrapped with op and
// the chain's kind. This is what disagreement C, D, E and F all
// rest on: the only branch that classifies is the one that sees a
// *pgconn.PgError or pgx.ErrNoRows; anything else flows through
// unchanged in kind.
func TestClassify_NotFoundFallsThroughWhenNotPgError(t *testing.T) {
	plain := errors.New("connection refused")
	got := pgxerr.Classify(plain, "create org", sentinelNotFound, sentinelConflict)
	require.Error(t, got)
	assert.True(t, errorx.IsKind(got, errorx.KindUnknown))
	assert.Equal(t, "create org: connection refused", got.Error())
}

// refusedDial answers what the driver returns when nothing accepts at
// the address it dials, under ctx: a listener opened and closed again
// leaves a port that refuses.
func refusedDial(t *testing.T, ctx context.Context) error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	_, err = pgconn.Connect(ctx, "postgres://garam@"+addr+"/garam?sslmode=disable&connect_timeout=5")
	var connectErr *pgconn.ConnectError
	require.ErrorAs(t, err, &connectErr, "the driver answers a refused dial with a ConnectError")
	return err
}

// resetSocket answers the error a write meets on a connection its peer
// reset: the peer closes with no linger, which sends a reset rather
// than an end of stream, and the write after it is refused by the
// socket. That is the *net.OpError the driver wraps as "write failed"
// or "failed to receive message" (#1546).
func resetSocket(t *testing.T) error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	client, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	peer, err := ln.Accept()
	require.NoError(t, err)
	tcp, ok := peer.(*net.TCPConn)
	require.True(t, ok, "a TCP listener accepts a *net.TCPConn")
	require.NoError(t, tcp.SetLinger(0))
	require.NoError(t, tcp.Close())
	for range 100 {
		if _, err = client.Write([]byte("Q")); err != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	var opErr *net.OpError
	require.ErrorAs(t, err, &opErr, "a write to a reset peer fails on the socket")
	return fmt.Errorf("write failed: %w", err)
}

// TestClassify_ALostConnectionIsUnavailable holds #1165's and #1546's
// mapping to the forms the driver gives an unreachable database, as a
// cut connection produced them against real Postgres: a ConnectError,
// io.ErrUnexpectedEOF on a connection that was open, and a socket error
// on one its peer reset. The controls are
// what must not read as an outage: the caller's own cancellation, in
// either form, and an error the driver did not raise.
func TestClassify_ALostConnectionIsUnavailable(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledDial := refusedDial(t, cancelled)
	require.ErrorIs(t, cancelledDial, context.Canceled, "the cancelled dial carries the caller's cancellation")
	for name, c := range map[string]struct {
		err  error
		want errorx.Kind
	}{
		"a dial nothing accepts":            {refusedDial(t, context.Background()), errorx.KindUnavailable},
		"an open connection ending":         {io.ErrUnexpectedEOF, errorx.KindUnavailable},
		"an open connection reset":          {resetSocket(t), errorx.KindUnavailable},
		"the caller cancelling a dial":      {cancelledDial, errorx.KindUnknown},
		"the caller cancelling an exchange": {context.Canceled, errorx.KindUnknown},
		"the caller's deadline passing":     {context.DeadlineExceeded, errorx.KindUnknown},
		"an error the driver did not raise": {errors.New("connection refused"), errorx.KindUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			got := pgxerr.Classify(c.err, "list policies", nil, nil)
			require.Error(t, got)
			assert.Equal(t, c.want, errorx.KindOf(got))
			assert.ErrorIs(t, got, c.err, "the driver's error stays on the chain for the log")
		})
	}
}

// TestClassify_PreservesKindInChain confirms the wrap preserves the
// chain's kind when no override is supplied. An errorx error wrapped
// further by Classify keeps its kind; that is what lets the caller
// add context without losing what an upper layer already decided.
func TestClassify_PreservesKindInChain(t *testing.T) {
	inner := errorx.New(errorx.KindPermissionDenied, "denied")
	got := pgxerr.Classify(inner, "create org", nil, nil)
	require.Error(t, got)
	assert.True(t, errorx.IsKind(got, errorx.KindPermissionDenied),
		"the chain's KindPermissionDenied survives a wrap with no override")
}

// TestIsForeignKeyViolation_ReadsTheCodeAndNotTheStatement covers the
// predicate a caller reaches for when 23503 means the opposite of what
// [pgxerr.Classify] maps it to. Three controls stand with the probe:
// another constraint code, an error the driver did not raise, and nil —
// each of which a predicate matching too widely would take.
//
// The wrapped row is the one that matters in practice: a store hands
// over what the driver returned through its own chain, so a predicate
// reading only the outermost error would answer false everywhere it is
// actually called.
func TestIsForeignKeyViolation_ReadsTheCodeAndNotTheStatement(t *testing.T) {
	tests := map[string]struct {
		err  error
		want bool
	}{
		"the foreign key code": {pgError("23503"), true},
		"wrapped in a chain":   {errorx.Wrap(pgError("23503"), "delete"), true},
		"another constraint":   {pgError("23505"), false},
		"not a driver error":   {errors.New("dial tcp: connection refused"), false},
		"no error at all":      {nil, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, pgxerr.IsForeignKeyViolation(tt.err))
		})
	}
}

// TestIsForeignKeyViolation_LeavesClassifyAlone holds the two readings
// apart in one place. The predicate exists because a delete needs the
// second one; every insert already calling Classify keeps the first,
// which message.Grant depends on (#703).
func TestIsForeignKeyViolation_LeavesClassifyAlone(t *testing.T) {
	err := pgError("23503")

	require.True(t, pgxerr.IsForeignKeyViolation(err))
	assert.True(t, errorx.IsKind(pgxerr.Classify(err, "grant consent", nil, nil), errorx.KindNotFound),
		"the insert reading is unchanged")
	assert.Same(t, sentinelNotFound, pgxerr.Classify(err, "grant consent", sentinelNotFound, nil),
		"and so is the caller sentinel it returns")
}
