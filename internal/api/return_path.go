package api

import (
	"strings"
	"unicode/utf8"
)

// maxReturnPath bounds a return path in UTF-8 bytes, the measure the
// console's own rule and the oauth_states column check use.
const maxReturnPath = 2048

// safeReturnPath answers path where it is one a login may land on: the
// console or a path below it, valid UTF-8 of at most maxReturnPath
// bytes, with no backslash a browser reads as a slash, no control
// character a browser drops, and no dot segment a browser resolves —
// any of which can carry a console path out of the console or to
// another origin. It is the rule `web/src/lib/return-path.ts` applies to
// the password sign-in, held here for the provider sign-in, whose
// redirect this process writes; `testdata/return-path-vectors.json` is
// the table both are tested against.
func safeReturnPath(path string) (string, bool) {
	if len(path) > maxReturnPath || !utf8.ValidString(path) {
		return "", false
	}
	if path != "/console" && !strings.HasPrefix(path, "/console/") {
		return "", false
	}
	if strings.ContainsFunc(path, func(r rune) bool {
		return r == '\\' || r < 0x20 || r == 0x7f
	}) {
		return "", false
	}
	if hasDotSegment(path) {
		return "", false
	}
	return path, true
}

// hasDotSegment reports whether the path part of p holds a `.` or `..`
// segment, written plainly or percent-encoded, as a browser's URL
// parser resolves both (WHATWG URL §4.4).
func hasDotSegment(p string) bool {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	for _, segment := range strings.Split(p, "/") {
		switch strings.ReplaceAll(strings.ToLower(segment), "%2e", ".") {
		case ".", "..":
			return true
		}
	}
	return false
}
