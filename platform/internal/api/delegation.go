package api

import (
	"net/http"
	"time"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// WriteOperatorDelegation grants or replaces a hosted operator's
// delegation (ADR-0078). Authorization is the service's, decided from
// current state rather than through the cached authorize.
func (s *Server) WriteOperatorDelegation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug, params apigen.WriteOperatorDelegationParams) {
	if !contract(w, params.ContractVersion) {
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	orgG := orgGRN(orgSlug)
	hosted, err := operator.GRNOf(orgG, operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.DelegationWrite
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	requestID, err := operator.ParseRequestID(body.RequestId)
	if err != nil {
		writeError(w, err)
		return
	}
	operations := make([]string, len(body.Operations))
	for i, op := range body.Operations {
		operations[i] = string(op)
	}
	terms, err := operator.ParseGrantTerms(orgG, hosted, body.Controllers, operations, body.ExpiresAt, time.Now())
	if err != nil {
		writeError(w, err)
		return
	}
	grant, created, err := s.authority.WriteGrant(r.Context(), operator.GrantWrite{
		Org: orgG, Operator: hosted, RequestID: requestID,
		Expected: (*operator.GrantID)(body.ExpectedGrantId), Terms: terms, Actor: u.GRN,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, delegationToAPI(grant))
}

// GetOperatorDelegation answers a hosted operator's current grant.
// Requires the member role. Reading moves nothing, so it is authorized
// as other reads are.
func (s *Server) GetOperatorDelegation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug, params apigen.GetOperatorDelegationParams) {
	if !contract(w, params.ContractVersion) {
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	orgG := orgGRN(orgSlug)
	if err := s.authorize(r, orgG, u, org.ActionOperatorRead); err != nil {
		writeError(w, err)
		return
	}
	hosted, err := operator.GRNOf(orgG, operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	grant, err := s.authority.CurrentGrant(r.Context(), orgG, hosted)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, delegationToAPI(grant))
}

// EndOperatorDelegation withdraws the grant the body names, and only
// that one.
func (s *Server) EndOperatorDelegation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug, params apigen.EndOperatorDelegationParams) {
	if !contract(w, params.ContractVersion) {
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	orgG := orgGRN(orgSlug)
	hosted, err := operator.GRNOf(orgG, operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.DelegationEnd
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.authority.EndGrant(r.Context(), u.GRN, orgG, hosted, operator.GrantID(body.GrantId)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetOperatorControlEndpoint registers where a hosted operator's
// control service answers the console (#1170). Authorization is the
// service's, decided from current state on operator:delegate rather
// than on the operator:update that renames one.
func (s *Server) SetOperatorControlEndpoint(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug, params apigen.SetOperatorControlEndpointParams) {
	if !contract(w, params.ContractVersion) {
		return
	}
	var body apigen.ControlEndpointWrite
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	endpoint, err := operator.ParseControlEndpoint(body.Url)
	if err != nil {
		writeError(w, err)
		return
	}
	s.setControlEndpoint(w, r, orgSlug, operatorSlug, &endpoint)
}

// ClearOperatorControlEndpoint clears a hosted operator's control
// endpoint, under the authority that sets one.
func (s *Server) ClearOperatorControlEndpoint(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug, params apigen.ClearOperatorControlEndpointParams) {
	if !contract(w, params.ContractVersion) {
		return
	}
	s.setControlEndpoint(w, r, orgSlug, operatorSlug, nil)
}

func (s *Server) setControlEndpoint(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug, endpoint *operator.ControlEndpoint) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	orgG := orgGRN(orgSlug)
	hosted, err := operator.GRNOf(orgG, operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	o, err := s.authority.SetControlEndpoint(r.Context(), u.GRN, orgG, hosted, endpoint)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operatorToAPI(o))
}

// MintOperationAuthority hands a delegated operator one operation on
// the signed-in user's behalf (ADR-0078).
func (s *Server) MintOperationAuthority(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.MintOperationAuthorityParams) {
	if !contract(w, params.ContractVersion) {
		return
	}
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.OperationAuthorityRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	req, err := parseMintRequest(u.GRN, orgGRN(orgSlug), body)
	if err != nil {
		writeError(w, err)
		return
	}
	minted, err := s.authority.Mint(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apigen.OperationAuthority{
		Authority:    minted.Authority,
		ExpiresAt:    minted.ExpiresAt,
		OperationRef: string(minted.Ref),
		GrantId:      string(minted.Grant),
	})
}

func parseMintRequest(actor, orgG grn.GRN, body apigen.OperationAuthorityRequest) (operator.MintRequest, error) {
	audience, err := grn.Parse(body.Audience)
	if err != nil {
		return operator.MintRequest{}, errorx.Wrap(err, "audience", errorx.KindInvalidArgument)
	}
	target, err := grn.Parse(body.Target)
	if err != nil {
		return operator.MintRequest{}, errorx.Wrap(err, "target", errorx.KindInvalidArgument)
	}
	op, err := operator.ParseOperation(string(body.Operation))
	if err != nil {
		return operator.MintRequest{}, err
	}
	requestID, err := operator.ParseRequestID(body.RequestId)
	if err != nil {
		return operator.MintRequest{}, err
	}
	digest, err := operator.ParseBodyDigest(body.BodySha256)
	if err != nil {
		return operator.MintRequest{}, err
	}
	var requestTarget *operator.RequestTarget
	if body.RequestTarget != nil {
		t, err := operator.ParseRequestTarget(*body.RequestTarget)
		if err != nil {
			return operator.MintRequest{}, err
		}
		requestTarget = &t
	}
	var destination *grn.GRN
	if body.Destination != nil {
		g, err := grn.Parse(*body.Destination)
		if err != nil {
			return operator.MintRequest{}, errorx.Wrap(err, "destination", errorx.KindInvalidArgument)
		}
		destination = &g
	}
	return operator.MintRequest{
		Actor: actor, Org: orgG, Audience: audience, Operation: op,
		Target: target, RequestID: requestID, Digest: digest, RequestTarget: requestTarget,
		Destination: destination,
	}, nil
}

// contract stamps the contract version on the answer and refuses a
// request that asked for another, before anything else is read.
func contract(w http.ResponseWriter, version apigen.ContractVersion) bool {
	w.Header().Set(operator.ContractVersionHeader, operator.ContractVersion)
	if version != operator.ContractVersion {
		writeError(w, operator.ErrUnsupportedContract)
		return false
	}
	return true
}

func delegationToAPI(g *operator.Grant) apigen.Delegation {
	controllers := make([]string, len(g.Controllers))
	for i, c := range g.Controllers {
		controllers[i] = c.String()
	}
	operations := make([]apigen.DelegatedOperation, len(g.Operations))
	for i, op := range g.Operations {
		operations[i] = apigen.DelegatedOperation(op)
	}
	return apigen.Delegation{
		GrantId:     string(g.ID),
		OperatorGrn: g.Operator.String(),
		Controllers: controllers,
		Operations:  operations,
		ExpiresAt:   g.ExpiresAt,
		GrantedBy:   g.GrantedBy.String(),
		GrantedAt:   g.GrantedAt,
	}
}
