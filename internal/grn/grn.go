// Package grn defines Garam Resource Names.
//
// A GRN identifies a resource inside garam.
//
//	grn:<organization>:<project>:<service_type>:<identifier>
//
// The organization and project segments identify the owning resources
// (ADR-0001). Examples:
//
//	grn:acme:default:user:alice
//	grn:acme:default:agent:bot-1
//	grn:acme:default:group:eng
//	grn:acme:default:project:frontend
//	grn:root:default:org:acme
package grn

import (
	"strings"

	"github.com/garamsh/gaon/internal/errorx"
)

const (
	prefix        = "grn"
	segmentsTotal = 5
)

// Reserved owner segments (ADR-0001).
const (
	// RootOwner owns root-level resources such as organizations. It
	// may not be used as a real organization identifier.
	RootOwner = "root"
	// DefaultProject marks org-level (project-less) ownership.
	DefaultProject = "default"
)

// GRN is a value-type resource identifier. Identity is by value; two
// GRN structs with equal fields are equal.
type GRN struct {
	Organization string
	Project      string
	ServiceType  string
	Identifier   string
}

// New constructs a GRN from its four required segments.
func New(organization, project, serviceType, identifier string) GRN {
	return GRN{
		Organization: organization,
		Project:      project,
		ServiceType:  serviceType,
		Identifier:   identifier,
	}
}

// String renders the GRN in canonical form.
func (g GRN) String() string {
	var b strings.Builder
	b.Grow(len(prefix) + len(g.Organization) + len(g.Project) + len(g.ServiceType) + len(g.Identifier) + 4)
	b.WriteString(prefix)
	b.WriteByte(':')
	b.WriteString(g.Organization)
	b.WriteByte(':')
	b.WriteString(g.Project)
	b.WriteByte(':')
	b.WriteString(g.ServiceType)
	b.WriteByte(':')
	b.WriteString(g.Identifier)
	return b.String()
}

// IsZero reports whether the GRN is the zero value.
func (g GRN) IsZero() bool {
	return g == GRN{}
}

// Validate returns nil if every segment is non-empty.
func (g GRN) Validate() error {
	switch {
	case g.Organization == "":
		return errorx.New(errorx.KindInvalidArgument, "grn: empty organization")
	case g.Project == "":
		return errorx.New(errorx.KindInvalidArgument, "grn: empty project")
	case g.ServiceType == "":
		return errorx.New(errorx.KindInvalidArgument, "grn: empty service type")
	case g.Identifier == "":
		return errorx.New(errorx.KindInvalidArgument, "grn: empty identifier")
	}
	return nil
}

// Parse parses a GRN from its canonical string form.
func Parse(s string) (GRN, error) {
	parts := strings.SplitN(s, ":", segmentsTotal)
	if len(parts) != segmentsTotal || parts[0] != prefix {
		return GRN{}, errorx.New(errorx.KindInvalidArgument, "grn: malformed string")
	}
	g := GRN{
		Organization: parts[1],
		Project:      parts[2],
		ServiceType:  parts[3],
		Identifier:   parts[4],
	}
	if err := g.Validate(); err != nil {
		return GRN{}, err
	}
	return g, nil
}
