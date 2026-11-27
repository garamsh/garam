package api

import "github.com/garamsh/garam/lib/grn"

// DeclareActions stands actions in for what the manifests declare, so a
// test reaches an app's requested actions before a manifest declares one
// an installation may be granted (ADR-0192 decision 6).
func (s *Server) DeclareActions(actions []grn.ActionInfo) { s.apps.declared = actions }
