package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/platform/internal/audit"
	"github.com/garamsh/garam/platform/internal/audit/postgres/db"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// CredentialStore writes and reads machine credential events
// (ADR-0114). Unlike RecordSend it holds a pool: a credential event is
// written by the route that answered, after the issuing store's own
// transaction committed, because the issuing stores are other
// domains' and a refusal has no transaction to share.
type CredentialStore struct {
	q *db.Queries
}

// NewCredentialStore constructs the store over pool.
func NewCredentialStore(pool *pgxpool.Pool) *CredentialStore {
	return &CredentialStore{q: db.New(pool)}
}

// RecordCredential writes one event.
func (s *CredentialStore) RecordCredential(ctx context.Context, c audit.Credential) error {
	params := db.RecordCredentialParams{
		SubjectGrn: c.Subject().String(),
		OrgGrn:     grn.Org(c.Subject().Organization).String(),
		Credential: string(c.Kind()),
		Action:     string(c.Action()),
		Outcome:    string(c.Outcome()),
		Replay:     c.Replay(),
		Serial:     optionalText(c.Serial()),
		NotBefore:  optionalTimestamptz(c.Validity().NotBefore),
		NotAfter:   optionalTimestamptz(c.Validity().NotAfter),
	}
	if reason, ok := c.Reason(); ok {
		params.Reason = optionalText(reason)
	}
	return pgxerr.Classify(s.q.RecordCredential(ctx, params), "record credential event", nil, nil)
}

// ListCredentials pages subject's events in its organization, newest
// first.
func (s *CredentialStore) ListCredentials(ctx context.Context, subject grn.GRN, f listfilter.Filter) ([]audit.RecordedCredential, error) {
	params := db.ListCredentialsParams{
		OrgGrn:     grn.Org(subject.Organization).String(),
		SubjectGrn: subject.String(),
		RowOffset:  int32(f.Offset),
	}
	if f.Limit > 0 {
		params.RowLimit = pgtype.Int4{Int32: int32(f.Limit), Valid: true}
	}
	rows, err := s.q.ListCredentials(ctx, params)
	if err != nil {
		return nil, pgxerr.Classify(err, "list credential events", nil, nil)
	}
	out := make([]audit.RecordedCredential, 0, len(rows))
	for _, row := range rows {
		g, err := grn.Parse(row.SubjectGrn)
		if err != nil {
			return nil, errorx.Wrap(err, "stored credential event subject", errorx.KindInternal)
		}
		var v audit.Validity
		if at := pgxtype.Time(row.NotBefore); at != nil {
			v.NotBefore = *at
		}
		if at := pgxtype.Time(row.NotAfter); at != nil {
			v.NotAfter = *at
		}
		e, err := audit.Reconstruct(g, audit.CredentialKind(row.Credential), audit.CredentialAction(row.Action),
			audit.Outcome(row.Outcome), row.Replay, row.Reason.String, row.Serial.String, v, row.RecordedAt.Time)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func optionalText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func optionalTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgxtype.Timestamptz(t)
}
