// Package grn forwards to lib/grn, where the implementation lives
// since a second module, cowork, needs it (ADR-0125, ADR-0132). It
// keeps this module's imports unchanged until #1659 rewrites them to
// the new path, and is deleted then; nothing new imports it.
package grn

import "github.com/garamsh/garam/lib/grn"

// GRN is [grn.GRN].
type GRN = grn.GRN

// The reserved segments and the organization's service type, as [grn]
// declares them.
const (
	RootOwner      = grn.RootOwner
	DefaultProject = grn.DefaultProject
	ServiceTypeOrg = grn.ServiceTypeOrg
)

// The constructors and the parser, as [grn] declares them.
var (
	New   = grn.New
	Org   = grn.Org
	Parse = grn.Parse
)
