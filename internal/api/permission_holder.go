package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// maxHolderCandidates bounds one page of listPermissionHolders: each
// candidate costs the engine one check, as one pair of
// checkPermissions does, and the bound is the same.
const maxHolderCandidates = maxPermissionChecks

// ListPermissionHolders answers which members and agents of the org
// the engine allows the action on the resource, a page of candidates
// at a time, each asked as enforcement asks it. Requires
// org:member-list; agents are asked only where the caller also holds
// agent:read.
func (s *Server) ListPermissionHolders(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListPermissionHoldersParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	check, err := permissionPair(params.Action, params.ResourceType, params.ResourceId)
	if err != nil {
		writeError(w, err)
		return
	}
	target, err := permissionTarget(orgSlug, check)
	if err != nil {
		writeError(w, err)
		return
	}
	limit, offset, err := holderPage(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	c, err := s.callerIn(r, g, u)
	if err != nil {
		writeError(w, err)
		return
	}
	// The check authorize makes for listOrgMembers, on the caller
	// already resolved.
	lists, err := s.allows(r, c, g, org.ActionOrgMemberList)
	if err != nil {
		writeError(w, err)
		return
	}
	if !lists {
		writeError(w, errorx.New(errorx.KindPermissionDenied, "insufficient role"))
		return
	}
	if org.IsAction(check.Action) && actionRules[check.Action].decidedBy != decidedByCheck {
		writeError(w, errorx.New(errorx.KindInvalidArgument, check.Action+" is decided at its operation, not by the engine alone"))
		return
	}
	candidates, err := s.holderCandidates(r, c)
	if err != nil {
		writeError(w, err)
		return
	}
	answer := apigen.PermissionHolders{Holders: []apigen.PermissionHolder{}}
	// An action garam does not define is held by no one, as
	// checkPermissions denies it to everyone.
	if !org.IsAction(check.Action) || offset >= len(candidates) {
		writeJSON(w, http.StatusOK, answer)
		return
	}
	end := min(offset+limit, len(candidates))
	if answer.Holders, err = s.holdersAmong(r, g, candidates[offset:end], target, check.Action); err != nil {
		writeError(w, err)
		return
	}
	if end < len(candidates) {
		answer.NextOffset = &end
	}
	writeJSON(w, http.StatusOK, answer)
}

// holderPage parses a page's bounds: limit defaults to, and may not
// exceed, maxHolderCandidates.
func holderPage(limit, offset *int) (int, int, error) {
	f, err := listfilter.Parse(nil, offset)
	if err != nil {
		return 0, 0, err
	}
	if limit == nil {
		return maxHolderCandidates, f.Offset, nil
	}
	if *limit < 1 || *limit > maxHolderCandidates {
		return 0, 0, errorx.New(errorx.KindOutOfRange, "limit must be between 1 and 50")
	}
	return *limit, f.Offset, nil
}

// holderCandidate is one principal a page may ask about: a member, or
// an agent where member is nil.
type holderCandidate struct {
	grn    grn.GRN
	member *org.Membership
}

// holderCandidates is the org's members in ascending GRN order, then,
// where c may read agents, its agents in the same order.
func (s *Server) holderCandidates(r *http.Request, c *caller) ([]holderCandidate, error) {
	members, err := s.orgs.ListMembers(r.Context(), c.org)
	if err != nil {
		return nil, err
	}
	candidates := make([]holderCandidate, 0, len(members))
	for _, m := range members {
		candidates = append(candidates, holderCandidate{grn: m.User, member: m})
	}
	readsAgents, err := s.allows(r, c, c.org, org.ActionAgentRead)
	if err != nil || !readsAgents {
		return candidates, err
	}
	agents, err := s.agents.ListByOrg(r.Context(), c.org, listfilter.Filter{})
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		candidates = append(candidates, holderCandidate{grn: a.GRN()})
	}
	return candidates, nil
}

// holdersAmong asks the engine about each candidate and answers those
// it allows. A member is asked as allows asks the signed-in caller; an
// agent holds no role, so it is asked with its groups and no
// statement, and the policies reaching it decide. A member whose user
// row is gone by now holds nothing: the delete cascades the membership
// away.
func (s *Server) holdersAmong(r *http.Request, orgGRN grn.GRN, candidates []holderCandidate, target grn.GRN, action string) ([]apigen.PermissionHolder, error) {
	var userGRNs []grn.GRN
	for _, cand := range candidates {
		if cand.member != nil {
			userGRNs = append(userGRNs, cand.grn)
		}
	}
	users, err := s.users.ListByGRNs(r.Context(), userGRNs)
	if err != nil {
		return nil, err
	}
	byGRN := make(map[grn.GRN]*user.User, len(users))
	for _, u := range users {
		byGRN[u.GRN] = u
	}
	holders := []apigen.PermissionHolder{}
	for _, cand := range candidates {
		groups, err := s.orgs.ListGroupsByMember(r.Context(), orgGRN, cand.grn)
		if err != nil {
			return nil, err
		}
		holder := apigen.PermissionHolder{Type: apigen.HolderAgent, Grn: cand.grn.String()}
		var allowed bool
		if cand.member == nil {
			allowed, err = s.evaluator.Evaluate(r.Context(), EvalInput{
				Subject: cand.grn, Object: target, Action: action, SubjectGroups: groups, SubjectOrg: orgGRN,
			})
		} else {
			u, ok := byGRN[cand.grn]
			if !ok {
				continue
			}
			nickname := u.Nickname
			holder.Type, holder.Nickname = apigen.HolderUser, &nickname
			allowed, err = s.allows(r, &caller{org: orgGRN, user: u, member: cand.member, groups: groups}, target, action)
		}
		if err != nil {
			return nil, err
		}
		if allowed {
			holders = append(holders, holder)
		}
	}
	return holders, nil
}
