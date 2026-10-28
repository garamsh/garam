package operator

import (
	"net/url"
	"strconv"
	"strings"

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
// builds under it: no host, a port outside 1 to 65535, credentials, a
// query, a fragment, a `.` or `..` path segment, a trailing slash, and
// any other spelling a URL parser would rewrite.
func ParseControlEndpoint(s string) (ControlEndpoint, error) {
	invalid := errorx.New(errorx.KindInvalidArgument,
		"control endpoint must be an https URL of at most 2048 bytes with a host and a valid port, and no credentials, query, fragment, dot segment or trailing slash")
	if len(s) > maxControlEndpoint {
		return "", invalid
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", invalid
	}
	switch {
	case u.Scheme != "https", u.Hostname() == "", !validPort(u.Port()), u.User != nil, u.Opaque != "",
		u.RawQuery != "", u.ForceQuery, u.Fragment != "", u.RawFragment != "",
		len(u.Path) > 0 && u.Path[len(u.Path)-1] == '/',
		hasDotSegment(u.EscapedPath()),
		u.String() != s:
		return "", invalid
	}
	return ControlEndpoint(s), nil
}

// validPort reports whether port is absent or a decimal from 1 to
// 65535.
func validPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == port
}

// hasDotSegment reports whether an escaped path holds a `.` or `..`
// segment, spelled plainly or percent-encoded. A URL parser removes
// those, so a path holding one is not the path that arrives.
func hasDotSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		switch strings.ReplaceAll(strings.ToLower(segment), "%2e", ".") {
		case ".", "..":
			return true
		}
	}
	return false
}
