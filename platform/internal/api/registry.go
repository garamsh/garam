package api

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
)

// GetRegistry answers the services the manifests declare with their
// types and actions (ADR-0143 item 6). It needs a session and nothing
// more: the answer is the generated catalogue's and holds nothing of an
// organization.
func (s *Server) GetRegistry(w http.ResponseWriter, r *http.Request) {
	if _, err := s.currentUser(r); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, registry())
}

// DescribeGrn answers what a GRN is: its service, type, parent and the
// actions that apply to its type. It reads no entity, so it never says
// whether the named thing exists.
func (s *Server) DescribeGrn(w http.ResponseWriter, r *http.Request, params apigen.DescribeGrnParams) {
	if _, err := s.currentUser(r); err != nil {
		writeError(w, err)
		return
	}
	description, err := describe(params.Grn)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, description)
}

// registry reads the catalogue into the answer of GetRegistry.
func registry() apigen.Registry {
	actions := grn.Actions()
	services := []apigen.RegistryService{}
	for _, name := range grn.Services() {
		entry := apigen.RegistryService{
			Name:    name,
			Types:   []apigen.RegistryType{},
			Actions: []apigen.RegistryAction{},
		}
		for typ, info := range grn.Types(name) {
			entry.Types = append(entry.Types, apigen.RegistryType{
				Name: typ, EntityType: info.Entity, Parents: nonNil(info.Parents),
			})
		}
		slices.SortFunc(entry.Types, func(a, b apigen.RegistryType) int { return cmp.Compare(a.Name, b.Name) })
		for _, action := range actions {
			if !strings.HasPrefix(action.Name, name+":") {
				continue
			}
			entry.Actions = append(entry.Actions, apigen.RegistryAction{
				Name: action.Name, Principals: nonNil(action.Principals), Resources: nonNil(action.Resources),
			})
		}
		slices.SortFunc(entry.Actions, func(a, b apigen.RegistryAction) int { return cmp.Compare(a.Name, b.Name) })
		services = append(services, entry)
	}
	return apigen.Registry{Services: services}
}

// describe reads one name through lib/grn. A name no manifest declares
// is answered as unknown; a string that is no GRN is the caller's
// error.
func describe(name string) (apigen.GrnDescription, error) {
	g, err := grn.Parse(name)
	if errors.Is(err, grn.ErrUndeclared) {
		return apigen.GrnDescription{Grn: name}, nil
	}
	if err != nil {
		return apigen.GrnDescription{}, err
	}
	entity := grn.Types(g.Service)[g.ServiceType].Entity
	applies := []string{}
	for _, action := range grn.Actions() {
		if slices.Contains(action.Resources, entity) {
			applies = append(applies, action.Name)
		}
	}
	slices.Sort(applies)
	d := apigen.GrnDescription{
		Grn: name, Known: true, Service: &g.Service, Type: &g.ServiceType,
		EntityType: &entity, Actions: &applies,
	}
	if parent := g.Parent(); parent != "" {
		d.Parent = &parent
	}
	if g.Locator != "" {
		d.Locator = &g.Locator
	}
	return d, nil
}

// nonNil answers a list that encodes as an empty array and not null.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return slices.Clone(list)
}
