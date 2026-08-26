// Package pgxerr maps postgres driver errors to errorx errors.
//
// The store error classifier used to be duplicated in five domain
// postgres stores (org, authz, ca, user, agent); the copies disagreed
// in signature, in which postgres codes they mapped and what they
// mapped them to, and in the message they wrapped the driver error
// with. They all do the same thing — read the sql state on a
// *pgconn.PgError, choose a kind, attach the operation the caller
// names — so they belong in one place. This is that place.
//
// classify is store-error mapping, not wire conversion: internal/wire
// owns a wire format and the two directions of its conversion, and the
// mapping here would choose a behaviour rather than convert one
// (docs/architecture/structure.md §When a new boundary is earned).
// errorx owns kinds and the chain that carries them, but it is
// transport-agnostic by design — its doc comment pins it that way so
// the same errors can flow over any surface — and adding a pgx
// dependency here would break that. The five copies are the evidence
// this mapping earns a boundary of its own.
package pgxerr

import (
	"errors"

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
// It answers what the driver said and not what it meant, because the two
// are not the same: 23503 on an insert means the row the new one names
// is missing, and on a delete it means a row still references the one
// being removed. [Classify] sees the code and never the statement, so a
// caller whose statement gives 23503 the second reading reads it here
// and answers for itself. That keeps the driver's own vocabulary inside
// this package, which is what it is for.
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
//	anything else            → wrapped with op and the chain's kind.
//
// The wrap message is op alone; the previous "<store> store: <op>"
// prefix was a duplicate of what the caller already knew.
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
	}
	return errorx.Wrap(err, op)
}
