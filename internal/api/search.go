package api

import (
	"net/http"
	"slices"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org"
)

// Bounds of searchOrg's limit: how many objects of each kind it
// answers.
const (
	defaultSearchLimit = 5
	maxSearchLimit     = 20
)

// searchKind is one kind searchOrg finds: the action its list is
// authorized on, on the organization, and how its objects are found,
// each already left out where the caller is denied its own read.
type searchKind struct {
	kind   apigen.FoundKindKind
	action string
	find   func(s *Server, r *http.Request, c *caller, n listfilter.Needle) ([]apigen.FoundObject, error)
}

// searchKinds is every kind searchOrg finds, in the order it answers
// them.
var searchKinds = []searchKind{
	{apigen.FoundPerson, org.ActionOrgMemberList, (*Server).searchPeople},
	{apigen.FoundAgent, org.ActionAgentRead, (*Server).searchAgents},
	{apigen.FoundTeam, org.ActionGroupRead, (*Server).searchTeams},
	{apigen.FoundPolicy, org.ActionPolicyRead, (*Server).searchPolicies},
	{apigen.FoundProject, org.ActionProjectRead, (*Server).searchProjects},
}

// SearchOrg finds the org's people, agents, teams, policies and
// projects whose name or identifier holds q, for the console's palette
// (#1583). Each kind is authorized as its list is, and a kind the
// caller may not list is left out.
func (s *Server) SearchOrg(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.SearchOrgParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	needle, err := listfilter.ParseNeedle(params.Q)
	if err != nil {
		writeError(w, err)
		return
	}
	limit, err := searchLimit(params.Limit)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := s.callerIn(r, orgGRN(orgSlug), u)
	if err != nil {
		writeError(w, err)
		return
	}
	found := make([]apigen.FoundKind, 0, len(searchKinds))
	for _, k := range searchKinds {
		allowed, err := s.allows(r, c, c.org, k.action)
		if err != nil {
			writeError(w, err)
			return
		}
		if !allowed {
			continue
		}
		objects, err := k.find(s, r, c, needle)
		if err != nil {
			writeError(w, err)
			return
		}
		shown := min(limit, len(objects))
		found = append(found, apigen.FoundKind{Kind: k.kind, Objects: objects[:shown], More: len(objects) - shown})
	}
	writeJSON(w, http.StatusOK, apigen.OrgSearch{Found: found})
}

// searchLimit parses searchOrg's limit, which defaults to
// defaultSearchLimit and may not exceed maxSearchLimit.
func searchLimit(limit *int) (int, error) {
	if limit == nil {
		return defaultSearchLimit, nil
	}
	if *limit < 1 || *limit > maxSearchLimit {
		return 0, errorx.New(errorx.KindOutOfRange, "limit must be between 1 and 20")
	}
	return *limit, nil
}

// searchPeople finds the org's members by nickname or identifier. A
// member has no read of its own, so none is left out past the list's
// own check.
func (s *Server) searchPeople(r *http.Request, c *caller, n listfilter.Needle) ([]apigen.FoundObject, error) {
	members, err := s.orgs.ListMembers(r.Context(), c.org)
	if err != nil {
		return nil, err
	}
	grns := make([]grn.GRN, 0, len(members))
	for _, m := range members {
		grns = append(grns, m.User)
	}
	users, err := s.users.SearchByGRNs(r.Context(), grns, n)
	if err != nil {
		return nil, err
	}
	out := make([]apigen.FoundObject, 0, len(users))
	for _, u := range users {
		nickname := u.Nickname
		out = append(out, apigen.FoundObject{Grn: u.GRN.String(), Name: &nickname})
	}
	return out, nil
}

// searchAgents finds the org's agents by their identifier or name,
// leaving out each one whose agent:read c is denied, as listAgents
// does.
func (s *Server) searchAgents(r *http.Request, c *caller, n listfilter.Needle) ([]apigen.FoundObject, error) {
	agents, err := s.agents.SearchByOrg(r.Context(), c.org, n)
	if err != nil {
		return nil, err
	}
	agents, err = visible(s, r, c, org.ActionAgentRead, agents, func(a *agent.Agent) BatchObject {
		return onItself(a.GRN())
	})
	if err != nil {
		return nil, err
	}
	out := make([]apigen.FoundObject, 0, len(agents))
	for _, a := range agents {
		found := apigen.FoundObject{Grn: a.GRN().String()}
		if name, ok := a.Name(); ok {
			n := string(name)
			found.Name = &n
		}
		out = append(out, found)
	}
	return out, nil
}

// searchTeams finds the org's groups by name or identifier, leaving out
// each one whose group:read c is denied, as listGroups does.
func (s *Server) searchTeams(r *http.Request, c *caller, n listfilter.Needle) ([]apigen.FoundObject, error) {
	groups, err := s.orgs.SearchGroups(r.Context(), c.org, n)
	if err != nil {
		return nil, err
	}
	groups, err = visible(s, r, c, org.ActionGroupRead, groups, func(gr *org.Group) BatchObject {
		return BatchObject{Object: gr.GRN, Statements: groupRoleStatements(gr, slices.Contains(c.groups, gr.GRN))}
	})
	if err != nil {
		return nil, err
	}
	out := make([]apigen.FoundObject, 0, len(groups))
	for _, gr := range groups {
		out = append(out, foundNamed(gr.GRN, gr.Name))
	}
	return out, nil
}

// searchPolicies finds the org's policies by name or identifier,
// leaving out each one whose policy:read c is denied, as listPolicies
// does.
func (s *Server) searchPolicies(r *http.Request, c *caller, n listfilter.Needle) ([]apigen.FoundObject, error) {
	policies, err := s.policies.SearchPolicies(r.Context(), c.org, n)
	if err != nil {
		return nil, err
	}
	policies, err = visible(s, r, c, org.ActionPolicyRead, policies, func(p *Policy) BatchObject { return onItself(p.GRN) })
	if err != nil {
		return nil, err
	}
	out := make([]apigen.FoundObject, 0, len(policies))
	for _, p := range policies {
		out = append(out, foundNamed(p.GRN, p.Name))
	}
	return out, nil
}

// searchProjects finds the org's projects by name or identifier,
// leaving out each one whose project:read c is denied, as listProjects
// does.
func (s *Server) searchProjects(r *http.Request, c *caller, n listfilter.Needle) ([]apigen.FoundObject, error) {
	projects, err := s.orgs.SearchProjects(r.Context(), c.org, n)
	if err != nil {
		return nil, err
	}
	mine, err := s.orgs.ListProjectsByMember(r.Context(), c.org, c.user.GRN)
	if err != nil {
		return nil, err
	}
	projects, err = visible(s, r, c, org.ActionProjectRead, projects, func(p *org.Project) BatchObject {
		return BatchObject{Object: p.GRN, Statements: projectRoleStatements(p, slices.Contains(mine, p.GRN))}
	})
	if err != nil {
		return nil, err
	}
	out := make([]apigen.FoundObject, 0, len(projects))
	for _, p := range projects {
		out = append(out, foundNamed(p.GRN, p.Name))
	}
	return out, nil
}

// foundNamed is a found object that always has a name.
func foundNamed(g grn.GRN, name string) apigen.FoundObject {
	return apigen.FoundObject{Grn: g.String(), Name: &name}
}
