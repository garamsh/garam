package machine

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/garamsh/garam/lib/errorx"
)

// NewServer returns the machine-facing HTTP server: it terminates TLS
// with the certificate that seam answers with and asks for no client
// certificate. A request presents a token, under [TokenScheme] or
// [BearerScheme]; tokens is asked for that request, and what it
// answers is the request's identity (ADR-0100, ADR-0118). A request
// presenting none carries no identity, which is how a caller reaches
// the routes that authenticate it by what its body or another header
// carries; what an admitted caller may do is decided by the identity
// it holds, and a request that carries none reaches no handler that
// asks for one.
//
// Each request it hands an identity is noted in evidence, here where
// that is decided (ADR-0109).
func NewServer(tokens Tokens, evidence *Evidence, certificate ServerCertificate, next http.Handler) *http.Server {
	// TLS 1.3 only: both ends of this surface are garam's, so neither
	// needs an older one.
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
	}
	// Asked per handshake rather than frozen into Certificates, so a
	// replaced certificate is presented on the next connection with
	// nothing restarted (ADR-0040).
	cfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return certificate.Get()
	}
	return &http.Server{
		Handler:   identify(tokens, evidence, next),
		TLSConfig: cfg,
	}
}

// ResolveDeadline bounds the read that resolves a request's token. It
// is the bound the lifecycle routes hold their own reads to
// (agent.LifecycleDeadline): a read that has not answered by then is a
// stalled store, and the request is answered unavailable rather than
// held until the store lets it go (#1613).
const ResolveDeadline = 10 * time.Second

// identify hands next the identity the request's token proved, read for
// this request (ADR-0100) within ResolveDeadline. A token that proves
// nothing leaves the request without an identity, and the reason
// travels beside it for the handler's refusal to carry. An identity it
// hands next is noted in evidence first.
func identify(tokens Tokens, evidence *Evidence, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, presented := PresentedToken(r)
		switch {
		case presented && len(r.Header.Values("Authorization")) > 1:
			refuse(w, r, next, errorx.New(errorx.KindUnauthenticated,
				"machine: a request presenting a token carries one Authorization header"))
		case presented:
			bearer, err := resolve(r.Context(), tokens, token)
			if err != nil {
				// A token that proves nothing is the caller's to fix and
				// is answered 401; only a read that could not be made is
				// the listener's anomaly.
				level := slog.LevelDebug
				if errorx.IsKind(err, errorx.KindUnavailable) {
					level = slog.LevelWarn
				}
				slog.Log(r.Context(), level, "token not resolved", slog.String("error", err.Error()))
				refuse(w, r, next, err)
				return
			}
			identity := Identity{grn: bearer.Subject, serial: bearer.Serial,
				notBefore: bearer.IssuedAt, notAfter: bearer.ExpiresAt}
			evidence.Note(r.Context(), Sighting{Caller: bearer.Subject, Path: PathToken})
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// resolve reads what token stands for within ResolveDeadline. A read
// the deadline ended is unavailable, whatever the store made of it; one
// the caller ended keeps its own error.
func resolve(ctx context.Context, tokens Tokens, token string) (Bearer, error) {
	bounded, cancel := context.WithTimeout(ctx, ResolveDeadline)
	defer cancel()
	bearer, err := tokens.Resolve(bounded, token)
	if err != nil && bounded.Err() != nil && ctx.Err() == nil {
		return Bearer{}, errorx.Wrap(err, "machine: token resolution deadline", errorx.KindUnavailable)
	}
	return bearer, err
}

// PresentedToken returns the token a request presents in any of its
// Authorization headers, under [TokenScheme] or its alias
// [BearerScheme] (ADR-0102), and whether it presents one. Another
// scheme is not a token, and a request presenting one presents none.
//
// It is the one reading of that header the listener and the routes
// share, so the scheme that decides a request's identity also decides
// its contract value and where its other values travel.
func PresentedToken(r *http.Request) (string, bool) {
	for _, value := range r.Header.Values("Authorization") {
		if token, ok := tokenIn(value); ok {
			return token, true
		}
	}
	return "", false
}

// tokenIn reads one Authorization value. The bearer scheme matches
// case-insensitively, as RFC 9110 has every scheme match, and a value
// naming it presents a token however malformed the rest is, so a bad
// one is refused rather than read as no token at all.
func tokenIn(value string) (string, bool) {
	if token, ok := strings.CutPrefix(value, TokenScheme+" "); ok {
		return token, true
	}
	scheme, token, _ := strings.Cut(value, " ")
	if strings.EqualFold(scheme, BearerScheme) {
		return token, true
	}
	return "", false
}

// refuse hands next the request with no identity and the reason it
// carries none.
func refuse(w http.ResponseWriter, r *http.Request, next http.Handler, err error) {
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), refusalKey{}, err)))
}

type identityKey struct{}

type refusalKey struct{}

// RefusalOf returns why a request that presented a token carries no
// identity: the token proved nothing, or could not be read, or arrived
// beside another Authorization header. A request that presented none
// answers nil, and its absence of identity is the handler's to decide
// on.
func RefusalOf(r *http.Request) error {
	err, ok := r.Context().Value(refusalKey{}).(error)
	if !ok {
		return nil
	}
	return err
}

// IdentityOf returns the machine identity the request authenticated
// as. The comma-ok is the whole of the distinction this listener
// makes: a request that carries no identity presented no token that
// proved one, and what such a request may reach is the handler's to
// decide.
func IdentityOf(r *http.Request) (Identity, bool) {
	identity, ok := r.Context().Value(identityKey{}).(Identity)
	return identity, ok
}
