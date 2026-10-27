package api

import (
	"net/http"
	"strings"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// sessionCookie is the platform's session cookie, which reaches cowork
// on a host the cookie is scoped to.
const sessionCookie = "garam_session"

// credentialOf is what the request presented, read as the platform
// reads it: a bearer alone where there is one, otherwise the cookie.
func credentialOf(r *http.Request) platform.Credential {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return platform.Credential{Bearer: token}
		}
		// A scheme the platform does not take is no credential; the
		// cookie beside it is not read in its place.
		return platform.Credential{}
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return platform.Credential{Session: c.Value}
	}
	return platform.Credential{}
}
