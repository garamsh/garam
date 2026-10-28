// Package usercontent serves what people and agents publish as HTML
// from an origin of its own (ADR-0127 §8). The origin carries no
// session: an address is a capability, signed by cowork's own origin
// once it has asked the platform whether the viewer may read the
// artifact, good for one version and a short while. What it serves runs
// in an iframe sandboxed without its origin, under a policy that lets
// it reach nothing, so a script in it can neither read a cowork
// session nor call cowork as its viewer.
package usercontent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/garamsh/garam/cowork/internal/blob"
	"github.com/garamsh/garam/lib/errorx"
)

// lifetime is how long an address is good for: long enough to load a
// page, short enough that one copied out of it soon stops working.
const lifetime = 10 * time.Minute

// minSecret is the shortest signing secret accepted, in bytes.
const minSecret = 32

// Path is where a version is served: its organization, artifact and
// number.
const Path = "GET /a/{org}/{artifact}/{version}"

// Signer signs and checks the addresses of versions.
type Signer struct {
	origin string
	secret []byte
	now    func() time.Time
}

// NewSigner answers a signer for origin, the user-content origin's URL
// as browsers reach it, under secret.
func NewSigner(origin string, secret []byte) (*Signer, error) {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return nil, errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("user-content origin %q is not an origin", origin))
	}
	if len(secret) < minSecret {
		return nil, errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("the user-content secret is at least %d bytes", minSecret))
	}
	return &Signer{origin: u.Scheme + "://" + u.Host, secret: secret, now: time.Now}, nil
}

// WithClock answers the signer reading the time from now, for a test
// that moves it.
func (s *Signer) WithClock(now func() time.Time) *Signer {
	c := *s
	c.now = now
	return &c
}

func (s *Signer) mac(org, id string, number int, exp int64) string {
	m := hmac.New(sha256.New, s.secret)
	_, _ = fmt.Fprintf(m, "%s\x00%s\x00%d\x00%d", org, id, number, exp)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// URL implements artifact.Viewer.
func (s *Signer) URL(org, id string, number int) string {
	exp := s.now().Add(lifetime).Unix()
	q := url.Values{"exp": {strconv.FormatInt(exp, 10)}, "sig": {s.mac(org, id, number, exp)}}
	return fmt.Sprintf("%s/a/%s/%s/%d?%s", s.origin, url.PathEscape(org), url.PathEscape(id), number, q.Encode())
}

// verify reports whether r's address is one URL signed and has not
// lapsed.
func (s *Signer) verify(org, id string, number int, r *http.Request) bool {
	exp, err := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
	if err != nil || s.now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(r.URL.Query().Get("sig")), []byte(s.mac(org, id, number, exp)))
}

// Versions answers an HTML version's bytes.
type Versions interface {
	Served(ctx context.Context, org, id string, number int) (blob.Blob, error)
}

// policy is the Content-Security-Policy every version is served under:
// inline script and style run, and nothing is fetched, framed, posted
// or navigated to. The sandbox directive holds even where the page is
// opened outside its iframe.
func policy(ancestors []string) string {
	frame := "'none'"
	if len(ancestors) > 0 {
		frame = strings.Join(ancestors, " ")
	}
	return strings.Join([]string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"style-src 'unsafe-inline'",
		"img-src data: blob:",
		"font-src data:",
		"media-src data: blob:",
		"connect-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
		"frame-ancestors " + frame,
		"sandbox allow-scripts",
	}, "; ")
}

// Handler serves versions whose address s signed, allowing them to be
// framed by ancestors, the origins of cowork's web app. It serves
// nothing else.
func (s *Signer) Handler(versions Versions, ancestors []string) http.Handler {
	csp := policy(ancestors)
	mux := http.NewServeMux()
	mux.HandleFunc(Path, func(w http.ResponseWriter, r *http.Request) {
		org, id := r.PathValue("org"), r.PathValue("artifact")
		number, err := strconv.Atoi(r.PathValue("version"))
		if err != nil || number < 1 || !s.verify(org, id, number, r) {
			http.Error(w, "this address is not one cowork signed, or it has lapsed", http.StatusForbidden)
			return
		}
		b, err := versions.Served(r.Context(), org, id, number)
		if errorx.IsKind(err, errorx.KindNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cache-Control", "private, max-age=600")
		_, _ = w.Write(b.Body)
	})
	return mux
}
