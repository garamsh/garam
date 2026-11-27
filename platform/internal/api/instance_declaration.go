package api

import (
	"encoding/json"
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/org"
)

// declarationCall is what every declaration route reads before it
// acts: the instance's GRN, the caller authorized to read the instance
// on its installation, and to place a declaration on the instance
// where declare is set (iam:bot.place, ADR-0192 decision 10) of an
// instance whose installation hosts bots, and the declarations.
func (s *Server) declarationCall(r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, declare bool) (grn.GRN, bot.DeclarationStore, error) {
	stores, i, err := s.instanceCall(r, orgSlug, installationSlug, instanceSlug, org.ActionInstanceRead)
	if err != nil {
		return grn.GRN{}, nil, err
	}
	if declare {
		u, err := s.currentUser(r)
		if err != nil {
			return grn.GRN{}, nil, err
		}
		if err := s.authorizeOn(r, orgGRN(orgSlug), i.GRN, u, org.ActionBotPlace); err != nil {
			return grn.GRN{}, nil, err
		}
		if err := bot.Hosts(r.Context(), s.decisions, i.Installation); err != nil {
			return grn.GRN{}, nil, err
		}
	}
	if stores.declarations == nil {
		return grn.GRN{}, nil, errorx.New(errorx.KindUnavailable, "declarations are not configured")
	}
	return i.GRN, stores.declarations, nil
}

// ListInstanceDeclarations answers the instance's live declarations.
func (s *Server) ListInstanceDeclarations(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, false)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := declarations.ListDeclarations(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	out := apigen.DeclarationList{Declarations: make([]apigen.Declaration, 0, len(list))}
	for _, d := range list {
		out.Declarations = append(out.Declarations, declarationToAPI(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateInstanceDeclaration places a new declaration under an
// identifier garam draws.
func (s *Server) CreateInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug) {
	s.declare(w, r, orgSlug, installationSlug, instanceSlug, "")
}

// PutInstanceDeclaration places a declaration under the identifier
// named, or its next generation.
func (s *Server) PutInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id apigen.DeclarationID) {
	if err := bot.CheckDeclarationID(id); err != nil {
		writeError(w, err)
		return
	}
	s.declare(w, r, orgSlug, installationSlug, instanceSlug, id)
}

// declare writes the body's object under id, or a drawn identifier
// where id is empty, answering 201 for a declaration placed and 200
// for a next generation.
func (s *Server) declare(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id string) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, true)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.DeclarationWrite
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	d, created, err := bot.Declare(r.Context(), declarations, g, id, body.Object)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, declarationToAPI(d))
}

// GetInstanceDeclaration answers one live declaration.
func (s *Server) GetInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id apigen.DeclarationID) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, false)
	if err != nil {
		writeError(w, err)
		return
	}
	if bot.CheckDeclarationID(id) != nil {
		writeError(w, bot.ErrDeclarationNotFound)
		return
	}
	d, err := declarations.GetDeclaration(r.Context(), g, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, declarationToAPI(d))
}

// DeleteInstanceDeclaration tombstones a declaration, which has the
// connector delete its object.
func (s *Server) DeleteInstanceDeclaration(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, installationSlug apigen.InstallationSlug, instanceSlug apigen.InstanceSlug, id apigen.DeclarationID) {
	g, declarations, err := s.declarationCall(r, orgSlug, installationSlug, instanceSlug, true)
	if err != nil {
		writeError(w, err)
		return
	}
	if bot.CheckDeclarationID(id) != nil {
		writeError(w, bot.ErrDeclarationNotFound)
		return
	}
	if _, err := declarations.Tombstone(r.Context(), g, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func declarationToAPI(d bot.Declaration) apigen.Declaration {
	return apigen.Declaration{
		Id:         d.ID,
		Instance:   d.Instance.String(),
		Generation: d.Generation,
		Object:     json.RawMessage(d.Object.JSON()),
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}
