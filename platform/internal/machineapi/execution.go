package machineapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/watch"
)

// The headers that carry an execution token and an operation authority
// beside the caller's token, which takes Authorization (ADR-0100).
const (
	executionHeader = "Garam-Execution"
	operationHeader = "Garam-Operation"
)

// ActivateAgent admits a runtime generation of an agent for the hosted
// control service calling (ADR-0078, ADR-0080). The caller's own
// credential and registration are read again first, as on every
// lifecycle decision; the agent's token, minted for the caller
// (ADR-0100), is proven by the execution service.
func (s *Server) ActivateAgent(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.ActivateAgentParams) {
	if !served(w, params.ExecutionContractVersion, executionValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, subject, ok := s.lifecycleParties(w, r, agentGRN)
	if !ok {
		return
	}
	var body machinegen.ActivationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "activation", errorx.KindInvalidArgument))
		return
	}
	in, err := activationInput(subject, body)
	if err != nil {
		writeError(w, err)
		return
	}
	res, err := s.executions.Activate(r.Context(), caller, in)
	if err != nil {
		writeExecutionError(w, r, err)
		return
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	a := res.Activation
	out := machinegen.Activation{
		ActivationId: a.ID.String(),
		Token:        res.Token.String(),
		TokenVersion: int(a.TokenVersion),
		Grn:          a.Agent.String(),
		Epoch:        strconv.Itoa(a.Epoch),
		Generation:   string(a.Generation),
	}
	if a.Lineage != nil {
		lineage := a.Lineage.String()
		out.Lineage = &lineage
	}
	writeJSON(w, status, out)
}

// activationInput parses what an activation request carries.
func activationInput(subject grn.GRN, body machinegen.ActivationRequest) (agent.ActivationInput, error) {
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		return agent.ActivationInput{}, err
	}
	epoch, err := parseEpoch(body.Epoch)
	if err != nil {
		return agent.ActivationInput{}, err
	}
	generation, err := agent.ParseGeneration(body.Generation)
	if err != nil {
		return agent.ActivationInput{}, err
	}
	in := agent.ActivationInput{
		Agent: subject, RequestID: requestID, Epoch: epoch, Generation: generation,
		OperationRef: body.OperationRef, Credential: agent.ForwardedToken(body.Token),
	}
	if body.ReplacesActivationId != nil {
		id, err := agent.ParseActivationID(*body.ReplacesActivationId)
		if err != nil {
			return agent.ActivationInput{}, err
		}
		in.Replaces = &id
	}
	return in, nil
}

// DeactivateAgent ends an activation if it is still the agent's current
// one, and answers the same when it already ended.
func (s *Server) DeactivateAgent(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, activationID machinegen.ActivationId, params machinegen.DeactivateAgentParams) {
	if !served(w, params.ExecutionContractVersion, executionValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, subject, ok := s.lifecycleParties(w, r, agentGRN)
	if !ok {
		return
	}
	id, err := agent.ParseActivationID(activationID)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.executions.Deactivate(r.Context(), caller, subject, id); err != nil {
		writeExecutionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// IntrospectAgentExecution answers whether a forwarded agent token and
// a generation are current, from one snapshot.
func (s *Server) IntrospectAgentExecution(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.IntrospectAgentExecutionParams) {
	if !served(w, params.ExecutionContractVersion, executionValues) {
		return
	}
	caller, subject, ok := s.lifecycleParties(w, r, agentGRN)
	if !ok {
		return
	}
	var body machinegen.ExecutionIntrospectionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "execution introspection", errorx.KindInvalidArgument))
		return
	}
	forwarded := agent.ForwardedToken(body.Token)
	var generation *agent.Generation
	if body.Generation != nil {
		g, err := agent.ParseGeneration(*body.Generation)
		if err != nil {
			writeError(w, err)
			return
		}
		generation = &g
	}
	in, err := s.executions.Introspect(r.Context(), caller, subject, forwarded, generation)
	if err != nil {
		writeExecutionError(w, r, err)
		return
	}
	out := machinegen.ExecutionIntrospection{
		Grn:        in.Agent.String(),
		Org:        in.Org.String(),
		Assignee:   in.Assignee.String(),
		Epoch:      strconv.Itoa(in.Epoch),
		Credential: machinegen.ExecutionIntrospectionCredential(in.Credential),
		Generation: machinegen.ExecutionIntrospectionGeneration(in.Generation),
	}
	if in.Current != nil {
		id := in.Current.String()
		out.ActivationId = &id
	}
	writeJSON(w, http.StatusOK, out)
}

// RecoverAgentCredential moves an agent to a new credential lineage
// under an administrator's handoff for exactly this request body. It
// signs nothing and answers the lineage alone (ADR-0100).
func (s *Server) RecoverAgentCredential(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.RecoverAgentCredentialParams) {
	if !served(w, params.ExecutionContractVersion, executionValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, subject, ok := s.lifecycleParties(w, r, agentGRN)
	if !ok {
		return
	}
	authority, ok := operationOf(r)
	if !ok {
		writeError(w, errorx.New(errorx.KindUnauthenticated,
			"a recovery carries its authority in a Garam-Operation header"))
		return
	}
	// The digest the handoff binds is of these exact bytes, so they are
	// kept as received and decoded from the copy.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, errorx.Wrap(err, "credential recovery", errorx.KindInvalidArgument))
		return
	}
	var body machinegen.CredentialRecoveryRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, errorx.Wrap(err, "credential recovery", errorx.KindInvalidArgument))
		return
	}
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		writeError(w, err)
		return
	}
	epoch, err := parseEpoch(body.Epoch)
	if err != nil {
		writeError(w, err)
		return
	}
	recovered, err := s.executions.Recover(r.Context(), caller, agent.RecoveryInput{
		Agent: subject, RequestID: requestID, Epoch: epoch, Authority: authority, Body: raw,
	})
	if err != nil {
		writeExecutionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, machinegen.RecoveredCredential{
		Grn: subject.String(), Lineage: recovered.Lineage.String(),
	})
}

// lifecycleParties answers the calling identity, read again for this
// decision, and the agent the path names. A token's dates and record
// were read for this request, and the recheck holds the decision to
// them as the decision is made (ADR-0100).
func (s *Server) lifecycleParties(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) (grn.GRN, grn.GRN, bool) {
	identity, ok := identityOf(w, r)
	if !ok {
		return grn.GRN{}, grn.GRN{}, false
	}
	subject, err := grn.Parse(agentGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "agent", errorx.KindInvalidArgument))
		return grn.GRN{}, grn.GRN{}, false
	}
	if !s.recheck(w, r, identity) {
		return grn.GRN{}, grn.GRN{}, false
	}
	return identity.GRN(), subject, true
}

// recheck reads the caller's credential validity and registration
// again for this decision, and writes the refusal itself.
func (s *Server) recheck(w http.ResponseWriter, r *http.Request, identity machine.Identity) bool {
	if err := s.rechecker.Recheck(r.Context(), identity); err != nil {
		err = withinBudget(r, err)
		if errorx.IsKind(err, errorx.KindNotFound) {
			err = errorx.Wrap(err, "the caller's registration is withdrawn", errorx.KindPermissionDenied)
		}
		writeError(w, err)
		return false
	}
	return true
}

// fenceOf answers the execution fence a request of an agent presents
// (ADR-0080): the agent token it authenticated with, whose serial and
// dates the fence judges (ADR-0100), and the execution token it
// carries in a Garam-Execution header, if any. A caller that is not an
// agent presents none, and nil is its answer.
func fenceOf(r *http.Request, identity machine.Identity) (*agent.Fencing, error) {
	if identity.GRN().ServiceType != agent.ServiceType {
		return nil, nil
	}
	fence := &agent.Fencing{
		Agent: identity.GRN(),
		Credential: agent.Credential{
			Serial: identity.Serial(), NotBefore: identity.NotBefore(), NotAfter: identity.NotAfter(),
		},
	}
	raw := r.Header.Get(executionHeader)
	if raw == "" {
		return fence, nil
	}
	token, err := agent.ParseToken(raw)
	if err != nil {
		return nil, errorx.Wrap(err, "execution token", errorx.KindUnauthenticated)
	}
	hash := token.Hash()
	fence.Token = &hash
	return fence, nil
}

// messageFence is the fence as the message domain declares it.
func messageFence(f *agent.Fencing) *message.Fence {
	if f == nil {
		return nil
	}
	return &message.Fence{Agent: f.Agent, Serial: f.Credential.Serial, NotBefore: f.Credential.NotBefore,
		NotAfter: f.Credential.NotAfter, TokenHash: f.TokenBytes()}
}

// watchFence is the fence as the watch domain declares it.
func watchFence(f *agent.Fencing) *watch.Fence {
	if f == nil {
		return nil
	}
	return &watch.Fence{Agent: f.Agent, Serial: f.Credential.Serial, NotBefore: f.Credential.NotBefore,
		NotAfter: f.Credential.NotAfter, TokenHash: f.TokenBytes()}
}

// fenced answers the caller and the fence its request presents, and
// writes the refusal itself for a request carrying neither.
func fenced(w http.ResponseWriter, r *http.Request) (grn.GRN, *agent.Fencing, bool) {
	identity, ok := callerCredential(w, r)
	if !ok {
		return grn.GRN{}, nil, false
	}
	fence, err := fenceOf(r, identity)
	if err != nil {
		writeError(w, err)
		return grn.GRN{}, nil, false
	}
	return identity.GRN(), fence, true
}

// parseEpoch reads an epoch carried as an opaque string. garam's own
// are decimal, so anything else is one garam never issued.
func parseEpoch(s string) (int, error) {
	epoch, err := strconv.Atoi(s)
	if err != nil || epoch < 1 || strconv.Itoa(epoch) != s {
		return 0, errorx.New(errorx.KindInvalidArgument, "epoch is not one garam issued")
	}
	return epoch, nil
}

// lifecycleBudget bounds an activation, a recovery or a deactivation
// by agent.LifecycleDeadline from before its first authority read, the
// caller's own recheck, to its commit.
func lifecycleBudget(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), agent.LifecycleDeadline)
	return r.WithContext(ctx), cancel
}

// withinBudget answers a failure the request's deadline caused as
// unavailable, whatever the dependency that ran out made of it.
func withinBudget(r *http.Request, err error) error {
	if err != nil && r.Context().Err() != nil {
		return errorx.Wrap(err, "lifecycle deadline", errorx.KindUnavailable)
	}
	return err
}

// writeExecutionError answers 422 for the refusal of what a request
// forwarded rather than how it was formed: a token that does not prove
// the agent.
func writeExecutionError(w http.ResponseWriter, r *http.Request, err error) {
	err = withinBudget(r, err)
	if errors.Is(err, agent.ErrTokenNotProven) {
		writeErrorStatus(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeError(w, err)
}
