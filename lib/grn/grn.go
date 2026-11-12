// Package grn defines Garam Resource Names.
//
// A GRN identifies a resource inside garam. It is an organization,
// the one service that owns the resource, one or more type and id
// pairs, and optionally a locator (ADR-0142).
//
//	grn:<org>/<service>/<type>/<id>[/<type>/<id>...][#<locator>]
//
// Examples:
//
//	grn:acme/iam/user/alice
//	grn:acme/iam/agent/bot-1
//	grn:root/iam/org/acme
//	grn:acme/cowork/project/frontend/channel/general
//	grn:acme/cowork/page/home#section-2
package grn

import (
	"sort"
	"strings"

	"github.com/garamsh/garam/lib/errorx"
)

const (
	prefix = "grn:"
	// segmentMax is the longest a segment may be (grn-model.md
	// §identifier).
	segmentMax = 64
	// nameMax is the longest the name before '#' may be, the prefix
	// included (ADR-0142 item 4).
	nameMax = 512
)

// ErrUndeclared is what [Parse] and [Validate] answer for a name whose
// service or last type no manifest declares, apart from the errors for
// a string that is no GRN. It is read with errors.Is.
var ErrUndeclared = errorx.New(errorx.KindInvalidArgument, "grn: invalid service type")

// RootOwner owns root-level resources such as organizations. It may
// not be used as a real organization identifier (ADR-0001).
const RootOwner = "root"

// ServiceTypeOrg is the type of an organization's GRN.
const ServiceTypeOrg = "org"

// TypeInfo is what a manifest says of one entity type a GRN names.
type TypeInfo struct {
	// Entity is the qualified Cedar entity type, as it is written on
	// the wire.
	Entity string
	// Parents are the qualified types an entity of this type may be a
	// member of.
	Parents []string
}

// ActionInfo is what a manifest says of one action.
type ActionInfo struct {
	// Name is the action as it is written on the wire,
	// `<service>:<type>-<verb>`.
	Name string
	// Principals and Resources are the qualified types it applies to.
	Principals []string
	Resources  []string
}

// catalogue is the one place the types a service owns are read from,
// keyed by service and then by type: what the manifests declare, which
// `make -C policy gen` writes into catalogue_gen.go. A service no
// manifest declares owns no type, and a name under it is refused. A
// type name is a service's own, so two services may each hold one of
// the same name.
var catalogue = buildCatalogue()

func buildCatalogue() map[string]map[string]bool {
	built := map[string]map[string]bool{}
	for service, types := range generatedTypes {
		built[service] = map[string]bool{}
		for name := range types {
			built[service][name] = true
		}
	}
	return built
}

// Services returns the services a manifest declares, sorted.
func Services() []string {
	names := make([]string, 0, len(generatedTypes))
	for service := range generatedTypes {
		names = append(names, service)
	}
	sort.Strings(names)
	return names
}

// Types returns what the manifest of service says of each of its
// entity types, by GRN type segment.
func Types(service string) map[string]TypeInfo {
	found := make(map[string]TypeInfo, len(generatedTypes[service]))
	for name, info := range generatedTypes[service] {
		found[name] = info
	}
	return found
}

// Actions returns every action a manifest declares with what it
// applies to.
func Actions() []ActionInfo {
	return append([]ActionInfo(nil), generatedActions...)
}

// ownerOf returns the one service whose catalogue holds typ, or the
// empty string where none does or where more than one does.
func ownerOf(typ string) string {
	owner := ""
	for service, types := range catalogue {
		if !types[typ] {
			continue
		}
		if owner != "" {
			return ""
		}
		owner = service
	}
	return owner
}

// GRN is a value-type resource identifier. Identity is by value; two
// GRN structs with equal fields are equal.
//
// Service is the one service that owns the resource. ServiceType and
// Identifier are the resource's own, the last type and id pair, and
// the type is one that service holds. Ancestors holds the pairs before
// it as "type/id[/type/id...]", empty when there are none.
type GRN struct {
	Organization string
	Service      string
	Ancestors    string
	ServiceType  string
	Identifier   string
	// Locator names a place inside the resource, as written after '#'
	// and escaped as RFC 3986 escapes a fragment.
	Locator string
}

// New constructs a GRN with no ancestors and no locator for a type
// that one service holds, and names that service. A type no service
// holds, or that more than one does, leaves the service empty and the
// GRN invalid; [NewIn] names the service for such a type.
func New(organization, serviceType, identifier string) GRN {
	return NewIn(organization, ownerOf(serviceType), serviceType, identifier)
}

// NewIn constructs a GRN with no ancestors and no locator under the
// named service.
func NewIn(organization, service, serviceType, identifier string) GRN {
	return GRN{
		Organization: organization,
		Service:      service,
		ServiceType:  serviceType,
		Identifier:   identifier,
	}
}

// Org returns the GRN of the organization identified by id. An
// organization is root-owned (ADR-0142 item 1), so id is also the
// organization segment of every resource beneath it.
func Org(id string) GRN {
	return New(RootOwner, ServiceTypeOrg, id)
}

// OrgPattern returns the authorization pattern that matches every
// resource the organization owns, "grn:<org>/*" (ADR-0142 item 1). A
// '*' in a pattern crosses '/' as it crossed ':' before.
func OrgPattern(organization string) string {
	return prefix + organization + "/*"
}

// String renders the GRN in canonical form.
func (g GRN) String() string {
	var b strings.Builder
	b.WriteString(g.name())
	if g.Locator != "" {
		b.WriteByte('#')
		b.WriteString(g.Locator)
	}
	return b.String()
}

// name renders the GRN as far as '#'.
func (g GRN) name() string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(g.Organization)
	b.WriteByte('/')
	b.WriteString(g.Service)
	b.WriteByte('/')
	if g.Ancestors != "" {
		b.WriteString(g.Ancestors)
		b.WriteByte('/')
	}
	b.WriteString(g.ServiceType)
	b.WriteByte('/')
	b.WriteString(g.Identifier)
	return b.String()
}

// Parent returns the name of the GRN with its last type and id pair
// removed and its locator dropped, and the empty string where the name
// holds one pair and so has no parent in the name. Where only the
// shared first project/<id> pair is left, that pair names the
// platform's project and no entity of the service holding it, so the
// parent is the project's own name, grn:<org>/iam/project/<id>
// (ADR-0142 item 3).
func (g GRN) Parent() string {
	if g.Ancestors == "" {
		return ""
	}
	service := g.Service
	if id, ok := strings.CutPrefix(g.Ancestors, "project/"); ok && !strings.Contains(id, "/") {
		service = "iam"
	}
	return prefix + g.Organization + "/" + service + "/" + g.Ancestors
}

// IsZero reports whether the GRN is the zero value.
func (g GRN) IsZero() bool {
	return g == GRN{}
}

// Validate returns nil if the GRN is one ADR-0142 allows: every
// segment is a slug (one to segmentMax characters of lowercase
// letters, digits and '-'), the type has an owning service, the
// ancestors are whole type and id pairs, the name before '#' is at
// most nameMax characters, and the locator is an RFC 3986 fragment
// (ADR-0142 item 5; grn-model.md §identifier).
//
// The set is what keeps a GRN whole wherever it is carried. ':' and
// '/' shift every segment after them, '?' and '#' move the tail of a
// URI into its query or fragment, and '*' and '?' are the wildcards
// an authorization pattern matches with (ADR-0002) — each renames the
// resource rather than failing on it.
func (g GRN) Validate() error {
	switch {
	case !isSlug(g.Organization):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid organization")
	case !catalogue[g.Service][g.ServiceType]:
		return ErrUndeclared
	case !validAncestors(g.Ancestors):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid ancestors")
	case !isSlug(g.Identifier):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid identifier")
	case len(g.name()) > nameMax:
		return errorx.New(errorx.KindInvalidArgument, "grn: name too long")
	case !validFragment(g.Locator):
		return errorx.New(errorx.KindInvalidArgument, "grn: invalid locator")
	}
	return nil
}

// validFragment reports whether s is an RFC 3986 fragment: pchar, '/'
// and '?' only, where a '%' begins a whole escape of two hex digits.
// The empty string is one; '#' is not a fragment character.
func validFragment(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("-._~!$&'()*+,;=:@/?", c) >= 0:
		case c == '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}
			i += 2
		default:
			return false
		}
	}
	return true
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// validAncestors reports whether a is empty or whole type and id
// pairs, each a slug.
func validAncestors(a string) bool {
	if a == "" {
		return true
	}
	parts := strings.Split(a, "/")
	if len(parts)%2 != 0 {
		return false
	}
	for _, p := range parts {
		if !isSlug(p) {
			return false
		}
	}
	return true
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

// Parse parses a GRN from its canonical string form. The five-segment
// form of ADR-0001 is refused.
func Parse(s string) (GRN, error) {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return GRN{}, errMalformed()
	}
	name, locator, hasLocator := strings.Cut(rest, "#")
	if hasLocator && locator == "" {
		return GRN{}, errMalformed()
	}
	// 2 for the organization and the service, 2 for the first pair.
	parts := strings.Split(name, "/")
	if len(parts) < 4 || len(parts)%2 != 0 {
		return GRN{}, errMalformed()
	}
	n := len(parts)
	g := GRN{
		Organization: parts[0],
		Service:      parts[1],
		Ancestors:    strings.Join(parts[2:n-2], "/"),
		ServiceType:  parts[n-2],
		Identifier:   parts[n-1],
		Locator:      locator,
	}
	if err := g.Validate(); err != nil {
		return GRN{}, err
	}
	return g, nil
}

func errMalformed() error {
	return errorx.New(errorx.KindInvalidArgument, "grn: malformed string")
}
