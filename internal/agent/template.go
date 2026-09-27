package agent

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ServiceTypeTemplate is the canonical GRN service-type for agent
// templates (ADR-0075).
const ServiceTypeTemplate = "agent-template"

// ErrTemplateNotFound is what a read, an edit or a delete of a template
// the organization does not hold answers.
var ErrTemplateNotFound = errorx.New(errorx.KindNotFound, "agent template not found")

// TemplateContent is what a template says: its name, what it is for,
// and the values a definition may copy from it. Each field is the
// parsed form, so a TemplateContent is within every bound a template
// row keeps.
type TemplateContent struct {
	Name        Name
	Description Description
	Values      Values
}

// Template is a starting point an organization keeps for defining
// agents (ADR-0075). It names no operator and no agent, and nothing
// records which definitions were started from it: a definition copies
// the values once, so editing or deleting a template reaches no agent.
type Template struct {
	grn       grn.GRN
	orgGRN    grn.GRN
	content   TemplateContent
	createdBy grn.GRN
	createdAt time.Time
	updatedAt time.Time
}

// NewTemplate mints the identity of a template orgGRN keeps and records
// what it says. createdBy is the member that created it.
func NewTemplate(orgGRN grn.GRN, content TemplateContent, createdBy grn.GRN) (*Template, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, errorx.Wrap(err, "agent template id")
	}
	// A template carrying no value holds an empty payload rather than
	// none, as a definition does.
	if content.Values == nil {
		content.Values = Values{}
	}
	now := time.Now().UTC()
	return &Template{
		grn:       grn.New(orgGRN.Identifier, grn.DefaultProject, ServiceTypeTemplate, hex.EncodeToString(b)),
		orgGRN:    orgGRN,
		content:   content,
		createdBy: createdBy,
		createdAt: now,
		updatedAt: now,
	}, nil
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

func (t *Template) GRN() grn.GRN             { return t.grn }
func (t *Template) Organization() grn.GRN    { return t.orgGRN }
func (t *Template) Content() TemplateContent { return t.content }

// CreatedBy returns the member that created the template.
func (t *Template) CreatedBy() grn.GRN   { return t.createdBy }
func (t *Template) CreatedAt() time.Time { return t.createdAt }
func (t *Template) UpdatedAt() time.Time { return t.updatedAt }
