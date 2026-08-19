package message

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message/db"
)

// Compile-time check that PostgresStore satisfies ConsentStore.
var _ ConsentStore = (*PostgresStore)(nil)

// Grant stands org's half of the pair. The statement decides which half
// moves from where the granter sorts, so the same call serves both
// organizations of a pair.
func (s *PostgresStore) Grant(ctx context.Context, org, other grn.GRN) (*Consent, error) {
	row, err := s.q.GrantConsent(ctx, db.GrantConsentParams{Org: org.String(), Other: other.String()})
	if err != nil {
		return nil, errorx.Wrap(err, "message store: grant consent")
	}
	return rowToConsent(row)
}

// Withdraw clears org's own half, and sweeps the row away when that was
// the last half standing: the table holds live consent only. The sweep
// rides the withdraw, as the retention sweep rides the enqueue
// (ADR-0034).
func (s *PostgresStore) Withdraw(ctx context.Context, org, other grn.GRN) error {
	arg := db.WithdrawConsentParams{Org: org.String(), Other: other.String()}
	if err := s.q.WithdrawConsent(ctx, arg); err != nil {
		return errorx.Wrap(err, "message store: withdraw consent")
	}
	if err := s.q.DropEmptyConsent(ctx, db.DropEmptyConsentParams(arg)); err != nil {
		return errorx.Wrap(err, "message store: drop empty consent")
	}
	return nil
}

// Stands answers the send path's one question. The two organizations of
// a ghost recipient's pair name no row, which reads as no consent —
// the refusal that follows is the routing failure ADR-0013 reports back
// as a message.
func (s *PostgresStore) Stands(ctx context.Context, a, b grn.GRN) (bool, error) {
	stands, err := s.q.ConsentStands(ctx, db.ConsentStandsParams{Org: a.String(), Other: b.String()})
	if err != nil {
		return false, errorx.Wrap(err, "message store: consent stands")
	}
	return stands, nil
}

// List reads every relation org is a side of.
func (s *PostgresStore) List(ctx context.Context, org grn.GRN) ([]*Consent, error) {
	rows, err := s.q.ListConsents(ctx, org.String())
	if err != nil {
		return nil, errorx.Wrap(err, "message store: list consents")
	}
	consents := make([]*Consent, 0, len(rows))
	for _, row := range rows {
		c, err := rowToConsent(row)
		if err != nil {
			return nil, err
		}
		consents = append(consents, c)
	}
	return consents, nil
}

// rowToConsent converts a sqlc row into the domain aggregate. A stored
// value that no longer parses is data corruption, hence internal.
func rowToConsent(row db.MessageConsent) (*Consent, error) {
	orgA, err := grn.Parse(row.OrgA)
	if err != nil {
		return nil, errorx.Wrap(err, "message store: consent org a", errorx.KindInternal)
	}
	orgB, err := grn.Parse(row.OrgB)
	if err != nil {
		return nil, errorx.Wrap(err, "message store: consent org b", errorx.KindInternal)
	}
	return NewConsent(orgA, orgB, consentStamp(row.AConsentedAt), consentStamp(row.BConsentedAt)), nil
}

// consentStamp reads a half's stamp, where a withdrawn half is no time
// at all rather than the zero time.
func consentStamp(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	stamp := t.Time
	return &stamp
}
