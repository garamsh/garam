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

	"github.com/garamsh/garam/internal/errorx"
)

const (
	prefix        = "grn"
	segmentsTotal = 5
	// segmentMax is the longest a segment may be (grn-model.md
	// §identifier).
	segmentMax = 64
)

// Reserved owner segments (ADR-0001).
const (
	// RootOwner owns root-level resources such as organizations. It
	// may not be used as a real organization identifier.
	RootOwner = "root"
	// DefaultProject marks org-level (project-less) ownership.
	DefaultProject = "default"
)

// ServiceTypeOrg is the service type of an organization's GRN.
const ServiceTypeOrg = "org"

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

// Org returns the GRN of the organization identified by id. An
// organization is root-owned (ADR-0001), so id is also the
// organization segment of every resource beneath it.
func Org(id string) GRN {
	return New(RootOwner, DefaultProject, ServiceTypeOrg, id)
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

// Validate returns nil if every segment is a slug: one to segmentMax
// characters of lowercase letters, digits and '-' (grn-model.md
// §identifier).
//
// The set is what keeps a GRN whole wherever it is carried. ':' shifts
// every segment after it, '?' and '#' move the tail of a URI into its
// query or fragment, and '*' and '?' are the wildcards an authorization
// pattern matches with (ADR-0002) — each renames the resource rather
// than failing on it.
func (g GRN) Validate() error {
	switch {
	case !isSlug(g.Organization):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid organization")
	case !isSlug(g.Project):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid project")
	case !isSlug(g.ServiceType):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid service type")
	case !isSlug(g.Identifier):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid identifier")
	}
	return nil
}

// isSlug reports whether s is a segment [Validate] accepts. Every
// character in the set is one ASCII byte, so any other byte — the lead
// byte of a multi-byte rune included — fails the loop, and the byte
// length is the character count.
func isSlug(s string) bool {
	if s == "" || len(s) > segmentMax {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}

// Parse parses a GRN from its canonical string form.
func Parse(s string) (GRN, error) {
	parts := strings.Split(s, ":")
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
