package postgres

import (
	"context"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/message/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// Compile-time check that Store satisfies ConsentStore.
var _ message.ConsentStore = (*Store)(nil)

// Grant stands org's half of the pair. The statement decides which half
// moves from where the granter sorts, so the same call serves both
// organizations of a pair.
func (s *Store) Grant(ctx context.Context, org, other grn.GRN) (*message.Consent, error) {
	row, err := s.q.GrantConsent(ctx, db.GrantConsentParams{Org: org.String(), Other: other.String()})
	if err != nil {
		return nil, pgxerr.Classify(err, "grant consent", nil, nil)
	}
	return rowToConsent(row)
}

// Withdraw clears org's own half, and sweeps the row away when that was
// the last half standing: the table holds live consent only. The sweep
// rides the withdraw, as the retention sweep rides the enqueue
// (ADR-0034).
func (s *Store) Withdraw(ctx context.Context, org, other grn.GRN) error {
	arg := db.WithdrawConsentParams{Org: org.String(), Other: other.String()}
	if err := s.q.WithdrawConsent(ctx, arg); err != nil {
		return pgxerr.Classify(err, "withdraw consent", nil, nil)
	}
	if err := s.q.DropEmptyConsent(ctx, db.DropEmptyConsentParams(arg)); err != nil {
		return pgxerr.Classify(err, "drop empty consent", nil, nil)
	}
	return nil
}

// Stands answers the send path's one question. The two organizations of
// a ghost recipient's pair name no row, which reads as no consent —
// the refusal that follows is the routing failure ADR-0013 reports back
// as a message.
func (s *Store) Stands(ctx context.Context, a, b grn.GRN) (bool, error) {
	stands, err := s.q.ConsentStands(ctx, db.ConsentStandsParams{Org: a.String(), Other: b.String()})
	if err != nil {
		return false, pgxerr.Classify(err, "consent stands", nil, nil)
	}
	return stands, nil
}

// List reads every relation org is a side of.
func (s *Store) List(ctx context.Context, org grn.GRN) ([]*message.Consent, error) {
	rows, err := s.q.ListConsents(ctx, org.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list consents", nil, nil)
	}
	consents := make([]*message.Consent, 0, len(rows))
	for _, row := range rows {
		c, err := rowToConsent(row)
		if err != nil {
			return nil, err
		}
		consents = append(consents, c)
	}
	return consents, nil
}

// CountByOrg counts the relations org is a side of, which deleting org
// removes. Each one is a pair, so what is counted here is also the
// other organization's half.
func (s *Store) CountByOrg(ctx context.Context, org grn.GRN) (int, error) {
	n, err := s.q.CountConsentsByOrg(ctx, org.String())
	if err != nil {
		return 0, pgxerr.Classify(err, "count consents by org", nil, nil)
	}
	return int(n), nil
}

// rowToConsent converts a sqlc row into the domain aggregate. A stored
// value that no longer parses is data corruption, hence internal.
func rowToConsent(row db.MessageConsent) (*message.Consent, error) {
	orgA, err := grn.Parse(row.OrgA)
	if err != nil {
		return nil, errorx.Wrap(err, "consent org a", errorx.KindInternal)
	}
	orgB, err := grn.Parse(row.OrgB)
	if err != nil {
		return nil, errorx.Wrap(err, "consent org b", errorx.KindInternal)
	}
	return &message.Consent{OrgA: orgA, OrgB: orgB, AConsentedAt: pgxtype.Time(row.AConsentedAt), BConsentedAt: pgxtype.Time(row.BConsentedAt)}, nil
}
