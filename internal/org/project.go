package org

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Project is a resource container that belongs to a single
// Organization. Every field is one the store writes and reads back, and
// the aggregate holds no invariant over any of them, so they are
// exported and whatever restores a row does it from outside this
// package (ADR-0069).
type Project struct {
	GRN          grn.GRN
	Name         string
	Organization grn.GRN
	Roles        []string
	Policies     []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewProject constructs a Project tied to the given Organization GRN.
// The optional roles are the ones the Project grants its members.
// CreatedAt and UpdatedAt are stamped from one instant.
func NewProject(g grn.GRN, name string, orgGRN grn.GRN, roles ...string) *Project {
	now := time.Now().UTC()
	return &Project{GRN: g, Name: name, Organization: orgGRN, Roles: roles, CreatedAt: now, UpdatedAt: now}
}
