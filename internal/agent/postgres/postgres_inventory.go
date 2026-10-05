package postgres

import (
	"context"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Inventory reads every source record of the organization from one
// snapshot and answers one record each, in the order agents,
// unclaimed definitions, templates, then the summary that counts them.
// It writes nothing and answers no value, only a digest of each.
func (s *Store) Inventory(ctx context.Context, orgGRN grn.GRN) ([]agent.InventoryRecord, agent.InventorySummary, error) {
	var records []agent.InventoryRecord
	summary := agent.InventorySummary{Record: "summary", Org: orgGRN.String()}
	err := s.inSnapshot(ctx, "inventory", func(q *db.Queries) error {
		agents, err := q.InventoryAgents(ctx, orgGRN.String())
		if err != nil {
			return storageError(err, "inventory agents", nil, nil)
		}
		attempts, err := q.InventoryAttempts(ctx, orgGRN.String())
		if err != nil {
			return storageError(err, "inventory attempts", nil, nil)
		}
		latest := make(map[string]*agent.InventoryAttempt, len(attempts))
		for _, a := range attempts {
			latest[a.AgentGrn] = &agent.InventoryAttempt{ImportID: agent.ImportID(a.ImportID), Stage: agent.CutoverStage(a.Stage)}
		}
		for _, row := range agents {
			r, err := inventoryAgent(row)
			if err != nil {
				return err
			}
			r.Attempt = latest[row.Grn]
			if r.HasDefinition {
				summary.Definitions++
			}
			records = append(records, r)
		}
		summary.Agents = len(agents)

		unclaimed, err := q.InventoryUnclaimedDefinitions(ctx, orgGRN.String())
		if err != nil {
			return storageError(err, "inventory definitions", nil, nil)
		}
		for _, row := range unclaimed {
			stored := agent.ReadStoredValues(row.AgentValues)
			records = append(records, agent.InventoryRecord{
				Record: agent.InventoryDefinition, GRN: row.AgentGrn, Name: row.Name,
				OperatorGRN: row.OperatorGrn, HasDefinition: true,
				Keys: stored.Keys, ValueSHA256: stored.ValueSHA256,
				Class: agent.CutoverUnclaimed, Disposition: agent.DispositionRetain,
			})
		}
		summary.Definitions += len(unclaimed)

		templates, err := q.InventoryTemplates(ctx, orgGRN.String())
		if err != nil {
			return storageError(err, "inventory templates", nil, nil)
		}
		for _, row := range templates {
			stored := agent.ReadStoredValues(row.AgentValues)
			records = append(records, agent.InventoryRecord{
				Record: agent.InventoryTemplate, GRN: row.Grn, Name: row.Name,
				Keys: stored.Keys, ValueSHA256: stored.ValueSHA256, Disposition: agent.DispositionRetain,
			})
		}
		summary.Templates = len(templates)
		return nil
	})
	if err != nil {
		return nil, agent.InventorySummary{}, err
	}
	return records, summary, nil
}

// inventoryAgent decides one agent's record: its source is its
// definition, or empty values under its assignee where it has none.
func inventoryAgent(row db.InventoryAgentsRow) (agent.InventoryRecord, error) {
	mode, err := agent.ParseMode(row.Mode)
	if err != nil {
		return agent.InventoryRecord{}, errorx.Wrap(err, "stored execution mode", errorx.KindInternal)
	}
	r := agent.InventoryRecord{
		Record: agent.InventoryAgent, GRN: row.Grn, Mode: mode,
		TrackedCredentials: row.TrackedCredentials,
		Keys:               []string{}, ValueSHA256: map[string]string{},
	}
	if l := lineageOf(row.Lineage); l != nil {
		r.Lineage = l.String()
	}
	if row.AssigneeGrn.Valid {
		r.Assignee, r.Epoch = row.AssigneeGrn.String, int(row.Epoch.Int32)
	}
	values, parsed := agent.Values{}, true
	if row.DefinitionOperatorGrn.Valid {
		stored := agent.ReadStoredValues(row.DefinitionValues)
		r.HasDefinition, r.Name, r.OperatorGRN = true, row.DefinitionName.String, row.DefinitionOperatorGrn.String
		r.Keys, r.ValueSHA256 = stored.Keys, stored.ValueSHA256
		values, parsed = stored.Values, stored.Values != nil
	} else {
		r.OperatorGRN = r.Assignee
	}
	r.Class = agent.ClassOf(mode, parsed)
	switch r.Class {
	case agent.CutoverEligible:
		r.Disposition = agent.DispositionImport
	case agent.CutoverUnparseable:
		r.Disposition = agent.DispositionPreserveAndBlock
	default:
		r.Disposition = agent.DispositionRetain
	}
	if !parsed || r.OperatorGRN == "" {
		return r, nil
	}
	agentGRN, err := grn.Parse(row.Grn)
	if err != nil {
		return agent.InventoryRecord{}, errorx.Wrap(err, "stored agent grn", errorx.KindInternal)
	}
	operatorGRN, err := grn.Parse(r.OperatorGRN)
	if err != nil {
		return agent.InventoryRecord{}, errorx.Wrap(err, "stored operator grn", errorx.KindInternal)
	}
	r.SourceDigest = agent.SourceDigest(agentGRN, operatorGRN, values).String()
	return r, nil
}
