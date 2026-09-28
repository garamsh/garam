package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
)

// Stored forms of a ConfigurationOutcome.
const (
	outcomeApplied  = "applied"
	outcomeRejected = "rejected"
)

// ReviseDefinition replaces the definition's values as a new revision,
// provided it still holds the revision expected. The comparison is in
// the statement, so of two saves against one revision the second
// matches no row.
func (s *PostgresStore) ReviseDefinition(ctx context.Context, agentGRN grn.GRN, expected Revision, values Values, by grn.GRN) (*Definition, error) {
	e, err := narrowRevision(expected)
	if err != nil {
		return nil, ErrRevisionConflict
	}
	if values == nil {
		values = Values{}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, errorx.Wrap(err, "revise definition: values", errorx.KindInternal)
	}
	row, err := s.q.ReviseAgentDefinitionValues(ctx, db.ReviseAgentDefinitionValuesParams{
		AgentValues:      payload,
		RevisedAt:        stamp(time.Now().UTC()),
		RevisedBy:        by.String(),
		AgentGrn:         agentGRN.String(),
		ExpectedRevision: e,
	})
	if err == nil {
		return rowToDefinition(row)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, pgxerr.Classify(err, "revise definition", nil, nil)
	}
	if _, getErr := s.q.GetAgentDefinition(ctx, agentGRN.String()); getErr != nil {
		return nil, pgxerr.Classify(getErr, "revise definition", ErrDefinitionNotFound, nil)
	}
	return nil, ErrRevisionConflict
}

// ReportConfiguration records the acknowledgement the operator
// assigned at epoch gives of revision.
func (s *PostgresStore) ReportConfiguration(ctx context.Context, agentGRN grn.GRN, epoch int, revision Revision, outcome ConfigurationOutcome) (*Agent, error) {
	e, err := narrowEpoch(epoch)
	if err != nil {
		return nil, err
	}
	r, err := narrowRevision(revision)
	if err != nil {
		return nil, ErrRevisionAhead
	}
	params := db.ReportAgentConfigurationParams{
		Grn:        agentGRN.String(),
		Epoch:      e,
		Revision:   r,
		ReportedAt: stamp(time.Now().UTC()),
	}
	switch o := outcome.(type) {
	case ConfigurationApplied:
		params.Outcome = outcomeApplied
		params.IgnoredKeys = o.IgnoredKeys
		if params.IgnoredKeys == nil {
			params.IgnoredKeys = []string{}
		}
	case ConfigurationRejected:
		params.Outcome = outcomeRejected
		params.Reason = pgtype.Text{String: string(o.Reason), Valid: true}
	default:
		return nil, errorx.New(errorx.KindInternal, "agent: unknown configuration outcome")
	}
	row, err := s.q.ReportAgentConfiguration(ctx, params)
	if err == nil {
		return rowToAgent(row)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, pgxerr.Classify(err, "report configuration", nil, nil)
	}
	return nil, s.refusedConfiguration(ctx, agentGRN, e, r)
}

// refusedConfiguration says why an acknowledgement matched no row, in
// the order the statement's conditions guard: the agent is gone, the
// epoch is not the current one, no definition describes the agent, the
// revision is one the definition has not reached, or the same epoch
// already acknowledged a later one. The reads are not the statement's
// snapshot, so a state that moved in between is named as it now is.
func (s *PostgresStore) refusedConfiguration(ctx context.Context, agentGRN grn.GRN, epoch, revision int32) error {
	const op = "report configuration"
	if _, err := s.q.GetAgent(ctx, agentGRN.String()); err != nil {
		return pgxerr.Classify(err, op, ErrAgentNotFound, nil)
	}
	assignment, err := s.q.GetAgentAssignment(ctx, agentGRN.String())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStaleEpoch
		}
		return pgxerr.Classify(err, op, nil, nil)
	}
	if assignment.Epoch != epoch {
		return ErrStaleEpoch
	}
	definition, err := s.q.GetAgentDefinition(ctx, agentGRN.String())
	if err != nil {
		return pgxerr.Classify(err, op, ErrDefinitionNotFound, nil)
	}
	if definition.Revision < revision {
		return ErrRevisionAhead
	}
	return ErrRevisionSuperseded
}

// narrowRevision converts a revision to the width of its column. One
// that does not fit is past any revision a definition holds.
func narrowRevision(r Revision) (int32, error) {
	n := int32(r)
	if int(n) != int(r) {
		return 0, ErrRevisionAhead
	}
	return n, nil
}

// rowToConfiguration reads the acknowledgement an agent row carries,
// or nil where it carries none. The schema keeps the columns whole, so
// a partial one is corruption.
func rowToConfiguration(row db.Agent) (*ConfigurationReport, error) {
	if !row.ConfigurationEpoch.Valid {
		return nil, nil
	}
	if !row.ConfigurationRevision.Valid || !row.ConfigurationOutcome.Valid || !row.ConfigurationReportedAt.Valid {
		return nil, errorx.New(errorx.KindInternal, "agent: partial configuration report")
	}
	revision, err := ParseRevision(int(row.ConfigurationRevision.Int32))
	if err != nil {
		return nil, err
	}
	var outcome ConfigurationOutcome
	switch row.ConfigurationOutcome.String {
	case outcomeApplied:
		applied, err := NewConfigurationApplied(row.ConfigurationIgnoredKeys)
		if err != nil {
			return nil, err
		}
		outcome = applied
	case outcomeRejected:
		reason, err := ParseRejectionReason(row.ConfigurationReason.String)
		if err != nil {
			return nil, err
		}
		outcome = ConfigurationRejected{Reason: reason}
	default:
		return nil, errorx.New(errorx.KindInternal, "agent: unknown configuration outcome")
	}
	return &ConfigurationReport{
		Epoch:      int(row.ConfigurationEpoch.Int32),
		Revision:   revision,
		Outcome:    outcome,
		ReportedAt: row.ConfigurationReportedAt.Time,
	}, nil
}
