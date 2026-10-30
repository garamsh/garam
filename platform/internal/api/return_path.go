package api

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// maxReturnPath bounds a return path in UTF-8 bytes, the measure the
// apps' own rule and the oauth_states column check use.
const maxReturnPath = 2048

// The apps the site hands a sign-in to (ADR-0135), as the login form
// names them. A form naming none is the console's.
const (
	appConsole = "console"
	appCowork  = "cowork"
)

// safeReturnPath answers path where it is one a login for app may land
// on: an absolute path with no host, inside the console for the
// console, valid UTF-8 of at most maxReturnPath bytes, with no
// backslash a browser reads as a slash, no control character a browser
// drops, and no dot segment a browser resolves — any of which can carry
// a path off its app or to another origin. It is the rule
// `lib/platform-sdk-ts/src/return-path.ts` applies to the password
// sign-in, held here for the provider sign-in, whose redirect this
// process writes; `testdata/return-path-vectors.json` is the table both
// are tested against.
func safeReturnPath(app, path string) (string, bool) {
	if len(path) > maxReturnPath || !utf8.ValidString(path) {
		return "", false
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}
	switch app {
	case appConsole:
		if path != "/console" && !strings.HasPrefix(path, "/console/") {
			return "", false
		}
	case appCowork:
	default:
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

// loginDestination is where on the site a provider sign-in for app
// lands, given the return path the form carried, and false for an app
// not on the list. The console's is the path itself, which the site
// sends on to the console. cowork's is the site's handoff for it,
// which, holding no state, sends the visitor to cowork to start one
// (ADR-0135). A path the rule drops, or one whose handoff would pass
// maxReturnPath, lands on the app's default.
func loginDestination(app, next string) (string, bool) {
	path, ok := safeReturnPath(app, next)
	if !ok {
		path = defaultReturnPath(app)
	}
	switch app {
	case appConsole:
		return path, true
	case appCowork:
		if dest := coworkHandoff(path); len(dest) <= maxReturnPath {
			return dest, true
		}
		return coworkHandoff(defaultReturnPath(app)), true
	}
	return "", false
}

// coworkHandoff is the site's handoff to cowork for path.
func coworkHandoff(path string) string {
	return "/handoff?" + url.Values{"app": {appCowork}, "next": {path}}.Encode()
}

// defaultReturnPath is where a sign-in for app lands without a return
// path.
func defaultReturnPath(app string) string {
	if app == appCowork {
		return "/"
	}
	return "/console"
}

// safeDestination answers stored where it is a destination
// loginDestination writes, so a row written by anything else cannot
// redirect off the apps: a console path, or the site's handoff for
// cowork carrying a cowork path, encoded exactly as loginDestination
// encodes it.
func safeDestination(stored string) (string, bool) {
	if path, ok := safeReturnPath(appConsole, stored); ok {
		return path, true
	}
	query, ok := strings.CutPrefix(stored, "/handoff?")
	if !ok {
		return "", false
	}
	values, err := url.ParseQuery(query)
	if err != nil || len(values) != 2 || values.Get("app") != appCowork {
		return "", false
	}
	if _, ok := safeReturnPath(appCowork, values.Get("next")); !ok || coworkHandoff(values.Get("next")) != stored {
		return "", false
	}
	return stored, true
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
