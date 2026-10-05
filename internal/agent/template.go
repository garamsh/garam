package agent

import (
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ServiceTypeTemplate is the canonical GRN service-type for agent
// templates (ADR-0075).
const ServiceTypeTemplate = "agent-template"

// ErrTemplateNotFound is what a read of a template the organization
// does not hold answers.
var ErrTemplateNotFound = errorx.New(errorx.KindNotFound, "agent template not found")

// TemplateContent is what a template says: its name, what it is for,
// and the values a definition could copy from it. Each field is the
// parsed form, so a TemplateContent is within every bound a template
// row keeps.
type TemplateContent struct {
	Name        Name
	Description Description
	Values      Values
}

// Template is a starting point an organization kept for defining
// agents (ADR-0075). It names no operator and no agent, and nothing
// records which definitions were started from it: a definition copied
// the values once. Templates are retired read-only, so a stored one is
// the record of what it said and nothing writes it again (ADR-0088).
//
// Its fields are exported: it is a data carrier with no invariant a
// method holds, so the field is the exposure (ADR-0069).
type Template struct {
	GRN    grn.GRN
	OrgGRN grn.GRN
	TemplateContent
	// CreatedBy is the member that created the template.
	CreatedBy grn.GRN
	CreatedAt time.Time
	UpdatedAt time.Time
}

// validateTemplateOwnership returns nil if g names a template of the
// organization orgGRN identifies.
func validateTemplateOwnership(g, orgGRN grn.GRN) error {
	switch {
	case g.ServiceType != ServiceTypeTemplate:
		return errorx.New(errorx.KindInvalidArgument, "agent: grn does not name an agent template")
	case g.Organization != orgGRN.Identifier:
		return errorx.New(errorx.KindInvalidArgument, "agent: agent template belongs to another organization")
	}
	return nil
}
