package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// IssueOperatorKey generates the operator a key and answers its
// private half once, with the identity and token URL its assertions
// name (ADR-0102).
//
// It is the one route that issues a key. The first key of an operator
// holding no live one is the registration's authority,
// operator:register; issuing beside or in place of a live key is
// operator:reissue, as withdrawing a key is. A caller holding
// only the first issues under KeyRotationFirst, which the store refuses
// for an operator that holds a live key in the transaction that would
// write, so a concurrent issuance cannot split the check from the
// write.
func (s *Server) IssueOperatorKey(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	reissue, err := s.authorizeKeyIssuance(r, orgSlug, u)
	if err != nil {
		writeError(w, err)
		return
	}
	g, err := operator.GRNOf(orgGRN(orgSlug), operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.OperatorKeyIssuance
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	rotation, err := keyRotation(body.Rotation)
	if err != nil {
		writeError(w, err)
		return
	}
	if !reissue {
		rotation = operator.KeyRotationFirst
	}
	if s.cfg.TokenURL == "" {
		writeError(w, operator.ErrKeyIssuanceUnconfigured)
		return
	}
	if err := s.keysPerProcess.Check(""); err != nil {
		writeError(w, err)
		return
	}
	if err := s.keysPerOperator.Check(g.String()); err != nil {
		writeError(w, err)
		return
	}
	issued, err := operator.NewIssuedKey(g, u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	key, err := s.operators.IssueKey(r.Context(), issued, rotation)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, apigen.IssuedOperatorKey{
		Key:           operatorKeyToAPI(key),
		PrivateKeyPem: string(issued.PrivatePEM()),
		ClientId:      g.String(),
		TokenUrl:      s.cfg.TokenURL,
	})
}

// authorizeKeyIssuance answers whether the caller holds
// operator:reissue, which issues under any rotation, refusing one that
// holds neither it nor operator:register, which issues a first key
// alone.
func (s *Server) authorizeKeyIssuance(r *http.Request, orgSlug string, u *user.User) (bool, error) {
	err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorReissue)
	if err == nil {
		return true, nil
	}
	if !errorx.IsKind(err, errorx.KindPermissionDenied) {
		return false, err
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorRegister); err != nil {
		return false, err
	}
	return false, nil
}

// keyRotation parses the rotation a request names.
func keyRotation(r apigen.OperatorKeyIssuanceRotation) (operator.KeyRotation, error) {
	switch r {
	case apigen.Planned:
		return operator.KeyRotationPlanned, nil
	case apigen.Immediate:
		return operator.KeyRotationImmediate, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "rotation is planned or immediate")
}

// ListOperatorKeys answers the public record of every key the operator
// was issued (ADR-0102). Requires operator:read.
func (s *Server) ListOperatorKeys(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
	g, ok := s.authorizedOperator(w, r, orgSlug, operatorSlug, org.ActionOperatorRead)
	if !ok {
		return
	}
	keys, err := s.operators.ListKeys(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	answer := apigen.OperatorKeyList{Keys: make([]apigen.OperatorKey, 0, len(keys))}
	for _, key := range keys {
		answer.Keys = append(answer.Keys, operatorKeyToAPI(key))
	}
	writeJSON(w, http.StatusOK, answer)
}

// RevokeOperatorKey withdraws the operator's key and every token
// exchanged under it (ADR-0102). Requires operator:reissue, which
// withdraws what the operator runs on as re-issuing does.
func (s *Server) RevokeOperatorKey(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug, kid apigen.KeyID) {
	g, ok := s.authorizedOperator(w, r, orgSlug, operatorSlug, org.ActionOperatorReissue)
	if !ok {
		return
	}
	if err := s.operators.RevokeKey(r.Context(), g, kid); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authorizedOperator authorizes action for the signed-in member and
// answers the operator the path names, writing the refusal itself
// where there is one.
func (s *Server) authorizedOperator(w http.ResponseWriter, r *http.Request, orgSlug, operatorSlug, action string) (grn.GRN, bool) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return grn.GRN{}, false
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, action); err != nil {
		writeError(w, err)
		return grn.GRN{}, false
	}
	g, err := operator.GRNOf(orgGRN(orgSlug), operatorSlug)
	if err != nil {
		writeError(w, err)
		return grn.GRN{}, false
	}
	return g, true
}

func operatorKeyToAPI(k operator.Key) apigen.OperatorKey {
	return apigen.OperatorKey{
		Kid:             k.KID,
		Algorithm:       apigen.RS256,
		State:           apigen.OperatorKeyState(k.State),
		IssuedAt:        k.IssuedAt,
		RetiresAt:       k.RetiresAt,
		RevokedAt:       k.RevokedAt,
		LastExchangedAt: k.LastExchangedAt,
	}
}
