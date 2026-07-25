package authz

import (
	"context"

	"github.com/garamsh/gaon/internal/grn"
)

// RoleAssignment records which roles a subject holds at a given scope.
// Scopes are either a Subject GRN (subject-scoped role) or an Object
// GRN (resource-scoped role). The same role may be assigned at
// different scopes.
type RoleAssignment struct {
	SubjectGRN grn.GRN
	Role       string
	Scope      grn.GRN
}

// RoleStore persists role assignments. Implementations live next to
// this file (memory.go, postgres.go).
type RoleStore interface {
	Assign(ctx context.Context, a RoleAssignment) error
	Unassign(ctx context.Context, a RoleAssignment) error

	// RolesOf returns every role a subject holds, regardless of scope.
	RolesOf(ctx context.Context, subjectGRN grn.GRN) ([]RoleAssignment, error)

	// RolesOn returns every subject that holds the given role on the
	// given scope.
	RolesOn(ctx context.Context, scope grn.GRN, role string) ([]grn.GRN, error)
}
