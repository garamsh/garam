package machineapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
)

// cutoverReasons is the closed set of reasons agent-cutover.v1 names
// on its refusals (ADR-0086). A refusal outside it carries none.
var cutoverReasons = []struct {
	err    error
	reason string
}{
	{agent.ErrCutoverDigestMismatch, "digest_mismatch"},
	{agent.ErrCutoverAttemptOpen, "attempt_open"},
	{agent.ErrCutoverAttemptEnded, "attempt_ended"},
	{agent.ErrCutoverNotEligible, "not_cut_over_eligible"},
	{agent.ErrReverseMigrationRequired, "reverse_migration_required"},
}

// GetAgentCutover answers what the Operator imports a legacy agent
// from, for a caller holding the agent's lifecycle authority and an
// agent:cutover reference (ADR-0086).
func (s *Server) GetAgentCutover(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.GetAgentCutoverParams) {
	if !served(w, r, params.CutoverContractVersion, cutoverValues) {
		return
	}
	caller, subject, ref, ok := s.cutoverParties(w, r, agentGRN)
	if !ok {
		return
	}
	c, err := s.executions.GetCutover(r.Context(), caller,
		agent.CutoverInput{Agent: subject, OperationRef: ref, RequestTarget: requestTarget(r)})
	if err != nil {
		writeCutoverError(w, r, err)
		return
	}
	out := machinegen.Cutover{
		Agent:    c.Agent.String(),
		Assignee: c.Assignee.String(),
		Epoch:    strconv.Itoa(c.Epoch),
		Mode:     machinegen.CutoverMode(c.Mode),
		Class:    machinegen.CutoverClass(c.Class),
	}
	if c.Source != nil {
		out.Source = &machinegen.CutoverSource{
			Operator:  c.Source.Operator.String(),
			Values:    c.Source.Values,
			DefinedBy: c.Source.DefinedBy.String(),
			CreatedAt: c.Source.CreatedAt,
		}
	}
	if c.SourceDigest != nil {
		digest := c.SourceDigest.String()
		out.SourceDigest = &digest
	}
	if c.Attempt != nil {
		attempt := attemptToAPI(c.Attempt)
		out.Attempt = &attempt
	}
	writeJSON(w, http.StatusOK, out)
}

// FreezeAgentCutover freezes a legacy agent's source once it matches
// the digest the Operator imported.
func (s *Server) FreezeAgentCutover(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.FreezeAgentCutoverParams) {
	if !served(w, r, params.CutoverContractVersion, cutoverValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, subject, ref, ok := s.cutoverParties(w, r, agentGRN)
	if !ok {
		return
	}
	var body machinegen.CutoverFreezeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "cutover freeze", errorx.KindInvalidArgument))
		return
	}
	in, err := freezeInput(subject, ref, requestTarget(r), body)
	if err != nil {
		writeError(w, err)
		return
	}
	attempt, created, err := s.executions.FreezeCutover(r.Context(), caller, in)
	if err != nil {
		writeCutoverError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, attemptToAPI(attempt))
}

func freezeInput(subject grn.GRN, ref, target string, body machinegen.CutoverFreezeRequest) (agent.FreezeInput, error) {
	importID, err := agent.ParseImportID(body.ImportId)
	if err != nil {
		return agent.FreezeInput{}, err
	}
	epoch, err := parseEpoch(body.Epoch)
	if err != nil {
		return agent.FreezeInput{}, err
	}
	digest, err := agent.ParseDigest(body.SourceDigest)
	if err != nil {
		return agent.FreezeInput{}, err
	}
	return agent.FreezeInput{
		CutoverInput: agent.CutoverInput{Agent: subject, ImportID: importID, OperationRef: ref, RequestTarget: target},
		Epoch:        epoch,
		SourceDigest: digest,
	}, nil
}

// SwitchAgentCutover records that the Operator owns a frozen agent's
// source.
func (s *Server) SwitchAgentCutover(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.SwitchAgentCutoverParams) {
	if !served(w, r, params.CutoverContractVersion, cutoverValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, in, ok := s.cutoverStage(w, r, agentGRN)
	if !ok {
		return
	}
	attempt, err := s.executions.SwitchCutover(r.Context(), caller, in)
	writeAttempt(w, r, attempt, err)
}

// RollBackAgentCutover ends a frozen attempt.
func (s *Server) RollBackAgentCutover(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.RollBackAgentCutoverParams) {
	if !served(w, r, params.CutoverContractVersion, cutoverValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, in, ok := s.cutoverStage(w, r, agentGRN)
	if !ok {
		return
	}
	attempt, err := s.executions.RollBackCutover(r.Context(), caller, in)
	writeAttempt(w, r, attempt, err)
}

// cutoverStage reads a stage whose body names only the import: the
// switch and the rollback.
func (s *Server) cutoverStage(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) (grn.GRN, agent.CutoverInput, bool) {
	caller, subject, ref, ok := s.cutoverParties(w, r, agentGRN)
	if !ok {
		return grn.GRN{}, agent.CutoverInput{}, false
	}
	var body machinegen.CutoverStageRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "cutover stage", errorx.KindInvalidArgument))
		return grn.GRN{}, agent.CutoverInput{}, false
	}
	importID, err := agent.ParseImportID(body.ImportId)
	if err != nil {
		writeError(w, err)
		return grn.GRN{}, agent.CutoverInput{}, false
	}
	return caller, agent.CutoverInput{Agent: subject, ImportID: importID, OperationRef: ref, RequestTarget: requestTarget(r)}, true
}

// writeAttempt answers a switch or a rollback.
func writeAttempt(w http.ResponseWriter, r *http.Request, attempt *agent.CutoverAttempt, err error) {
	if err != nil {
		writeCutoverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, attemptToAPI(attempt))
}

// cutoverParties answers the caller, read again for this decision, the
// agent the path names and the agent:cutover reference the request
// carries: as `Authorization: Garam-Operation <ref>` beside a
// certificate, and in a Garam-Operation header beside a token
// (ADR-0100).
func (s *Server) cutoverParties(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) (grn.GRN, grn.GRN, string, bool) {
	caller, subject, ok := s.lifecycleParties(w, r, agentGRN)
	if !ok {
		return grn.GRN{}, grn.GRN{}, "", false
	}
	ref, ok := operationOf(r)
	if !ok {
		writeError(w, errorx.New(errorx.KindUnauthenticated,
			"a cutover carries its agent:cutover reference as Authorization: Garam-Operation <reference> beside a certificate, or in a Garam-Operation header beside a token"))
		return grn.GRN{}, grn.GRN{}, "", false
	}
	return caller, subject, ref, true
}

// requestTarget is the request target r arrived at: its escaped path
// and raw query, as an agent:cutover reference binds it. Each cutover
// route is served under one method, so the path names the stage.
func requestTarget(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return r.URL.EscapedPath()
	}
	return r.URL.EscapedPath() + "?" + r.URL.RawQuery
}

func attemptToAPI(a *agent.CutoverAttempt) machinegen.CutoverAttempt {
	return machinegen.CutoverAttempt{
		ImportId:     string(a.ImportID),
		Stage:        machinegen.CutoverAttemptStage(a.Stage),
		FrozenDigest: a.FrozenDigest.String(),
		CreatedAt:    a.CreatedAt,
		SwitchedAt:   a.SwitchedAt,
		EndedAt:      a.EndedAt,
	}
}

// writeCutoverError answers a cutover refusal with the reason its
// contract names for it, 422 for an agent that cannot cut over, and
// every other refusal as the execution routes do.
func writeCutoverError(w http.ResponseWriter, r *http.Request, err error) {
	err = withinBudget(r, err)
	for _, c := range cutoverReasons {
		if errors.Is(err, c.err) {
			status := wire.StatusOf(errorx.KindOf(err))
			if c.err == agent.ErrCutoverNotEligible {
				status = http.StatusUnprocessableEntity
			}
			writeErrorReason(w, status, err, c.reason)
			return
		}
	}
	writeExecutionError(w, r, err)
}
