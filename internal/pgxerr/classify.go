// Package pgxerr maps postgres driver errors to errorx errors: it reads
// the sql state on a *pgconn.PgError, chooses a kind, and attaches the
// operation the caller names. Every postgres-backed store answers
// through it (docs/architecture/domains.md §Shared code below the
// domains).
package pgxerr

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/garamsh/garam/internal/errorx"
)

// Postgres sqlstate codes the unified classifier handles by name. They
// are spelled in the standard's exact form so a rename of the constant
// the driver exports later cannot silently lose a mapping.
const (
	codeUniqueViolation = "23505" // 23505 unique_violation
	codeForeignKey      = "23503" // 23503 foreign_key_violation
	codeCheckViolation  = "23514" // 23514 check_violation
	codeRaiseException  = "P0001" // P0001 raise_exception
)

// IsForeignKeyViolation reports whether err is postgres refusing a
// statement on a foreign key.
//
// It answers what the driver said and not what it meant: 23503 on an
// insert means the row the new one names is missing, and on a delete it
// means a row still references the one being removed. [Classify] sees
// the code and never the statement, so a caller whose statement gives
// 23503 the second reading reads it here and answers for itself.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == codeForeignKey
}

// Classify maps a postgres driver error to an errorx error. op is the
// caller's own label for the operation (e.g. "create org"); it
// appears in the wrap message so the operation is recoverable from
// the error chain alone.
//
//	nil                     → nil
//	pgx.ErrNoRows           → notFound if non-nil, otherwise wrapped
//	                          with op and the chain's kind.
//	23505 (unique_violation) → conflict if non-nil, otherwise wrapped
//	                          with KindAlreadyExists.
//	23503 (foreign_key)      → notFound if non-nil, otherwise wrapped
//	                          with "<op>: referenced row missing" and
//	                          KindNotFound.
//	23514 (check_violation)  → KindInvalidArgument: a value outside
//	                          the column's check is a caller mistake,
//	                          not an outage.
//	P0001 (raise_exception)  → KindFailedPrecondition: a trigger that
//	                          fires is a system-state refusal.
//	a lost connection        → KindUnavailable; see [connectionLost].
//	anything else            → wrapped with op and the chain's kind.
func Classify(err error, op string, notFound, conflict error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if notFound != nil {
			return notFound
		}
		return errorx.Wrap(err, op)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case codeUniqueViolation:
			if conflict != nil {
				return conflict
			}
			return errorx.Wrap(err, op, errorx.KindAlreadyExists)
		case codeForeignKey:
			if notFound != nil {
				return notFound
			}
			return errorx.Wrap(err, op+": referenced row missing", errorx.KindNotFound)
		case codeCheckViolation:
			return errorx.Wrap(err, op, errorx.KindInvalidArgument)
		case codeRaiseException:
			return errorx.Wrap(err, op, errorx.KindFailedPrecondition)
		}
		return errorx.Wrap(err, op)
	}
	if connectionLost(err) {
		return errorx.Wrap(err, op, errorx.KindUnavailable)
	}
	return errorx.Wrap(err, op)
}

// connectionLost reports whether err is the database becoming
// unreachable, in the forms the driver gives it: a
// *pgconn.ConnectError, where no connection could be made (#1165);
// io.ErrUnexpectedEOF, where an established one ended mid-exchange
// (#1165); and a *net.OpError, where the socket under an established
// one failed — a peer's reset answers a write or a read with
// ECONNRESET rather than an EOF (#1546). A chain carrying the caller's
// context error is the caller ending the call, not an outage, and
// Classify reaches here only for a chain with no SQLSTATE: a server
// that answered with one was reachable.
func connectionLost(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var connectErr *pgconn.ConnectError
	var socketErr *net.OpError
	return errors.As(err, &connectErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &socketErr)
}
