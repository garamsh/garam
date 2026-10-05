package operator

import (
	"net/url"

	"github.com/garamsh/garam/internal/errorx"
)

// ControlEndpoint is the base URL where a hosted operator's control
// service answers the console (#1170). The console sends an operation
// authority to no other origin, and builds every request URL under it,
// so a path prefix it carries reaches the control service unchanged.
type ControlEndpoint string

// maxControlEndpoint bounds an endpoint in bytes. migrations/000032
// holds the same bound.
const maxControlEndpoint = 2048

// ParseControlEndpoint parses an absolute https URL with an optional
// path prefix. It refuses what would change the request a console
// builds under it: credentials, a query, a fragment, a trailing slash,
// and any spelling a URL parser would rewrite.
func ParseControlEndpoint(s string) (ControlEndpoint, error) {
	invalid := errorx.New(errorx.KindInvalidArgument,
		"control endpoint must be an https URL of at most 2048 bytes, with no credentials, query, fragment or trailing slash")
	if len(s) > maxControlEndpoint {
		return "", invalid
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", invalid
	}
	switch {
	case u.Scheme != "https", u.Host == "", u.User != nil, u.Opaque != "",
		u.RawQuery != "", u.ForceQuery, u.Fragment != "", u.RawFragment != "",
		len(u.Path) > 0 && u.Path[len(u.Path)-1] == '/',
		u.String() != s:
		return "", invalid
	}
	return ControlEndpoint(s), nil
}
