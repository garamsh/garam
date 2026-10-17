package machine

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// NewServer returns the machine-facing HTTP server: it terminates TLS
// with the certificate that seam answers with, asks for a client
// certificate, and hands next the identity that certificate proved
// (ADR-0014). The chain is verified in next's Service and not by the
// TLS stack, because which authority may sign is decided by the
// organization the certificate's own SAN names.
//
// A connection whose certificate does not authenticate is not refused
// here, and carries no identity instead. That is what leaves room for
// the one route a caller reaches before it has a certificate at all —
// an operator enrolling with a token (ADR-0064) — and it costs nothing
// elsewhere: what an admitted caller may do is decided by the identity
// it holds, and a request that carries none reaches no handler that
// asks for one.
//
// A request may present a token instead, under [TokenScheme] or
// [BearerScheme]: then
// tokens is asked for that request, and what it answers is the
// request's identity (ADR-0100). A request presenting a token over a
// connection whose certificate authenticated is refused rather than
// read as either, because two credentials naming two identities have
// no single answer to who is calling.
//
// Each request it hands an identity is noted in evidence by the method
// that proved it, here where that is decided (ADR-0109).
func NewServer(s Service, tokens Tokens, evidence *Evidence, certificate ServerCertificate, next http.Handler) *http.Server {
	// TLS 1.3 only: both ends of this surface are garam's, so neither
	// needs an older one.
	// Every connection is served a clone of base, so base names the
	// ALPN protocols itself: ServeTLS adds them to a config of its own
	// (go1.27.2 net/http), which the clone never sees.
	base := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.RequestClientCert,
		NextProtos: []string{"h2", "http/1.1"},
	}
	// Asked per handshake rather than frozen into Certificates, so a
	// replaced certificate is presented on the next connection with
	// nothing restarted (ADR-0040). The client half below is untouched
	// by it: TLS 1.3 sends the server's certificate before the client
	// sends one, so nothing here reads what the caller presented.
	base.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return certificate.Get()
	}
	// Per connection, because the handshake's context reaches the
	// callback below no other way: a lookup then ends with the
	// connection that needed it instead of outliving it, and the cell
	// the outcome is written to is the one this connection's requests
	// read.
	base.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		ctx := hello.Context()
		cfg := base.Clone()
		cfg.GetConfigForClient = nil
		// VerifyConnection rather than VerifyPeerCertificate, because
		// this one also runs on a resumed handshake. A TLS 1.3 ticket
		// restores the peer certificates without re-running the
		// certificate callbacks, so a resumed connection would carry no
		// identity at all and every request on it would be refused.
		cfg.VerifyConnection = func(state tls.ConnectionState) error {
			chain := chainOf(state)
			identity, err := s.Authenticate(ctx, chain)
			if err != nil {
				// The handshake completes and the connection stays
				// anonymous. Refusing here instead would take the
				// enrollment route down with every caller that has no
				// certificate yet, which is all of them.
				recordNoIdentity(ctx, chain, err)
				return nil
			}
			if cell, ok := authenticatedOf(ctx); ok {
				cell.identity = &identity
			}
			return nil
		}
		return cfg, nil
	}
	return &http.Server{
		Handler:   identify(tokens, evidence, next),
		TLSConfig: base,
		// The cell has to exist before the handshake writes to it, and
		// this is the one hook that runs first: net/http derives the
		// connection's context here and hands that same context to the
		// TLS handshake and to every request served over it.
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, authenticatedKey{}, &authenticated{})
		},
	}
}

// chainOf renders the certificates a connection presented in the form
// [Service.Authenticate] reads them, leaf first, as TLS hands them
// over. A connection that presented none yields none.
func chainOf(state tls.ConnectionState) [][]byte {
	chain := make([][]byte, 0, len(state.PeerCertificates))
	for _, certificate := range state.PeerCertificates {
		chain = append(chain, certificate.Raw)
	}
	return chain
}

// recordNoIdentity writes down why a connection carries no identity.
// The listener's own log is the only place that reason is kept: what
// reaches the caller is that its certificate did not authenticate,
// and nothing about which organizations exist.
//
// A connection that presented no certificate is how a caller reaches
// enrollment (ADR-0064), so it is ordinary traffic and is recorded at
// info. One that presented a certificate which did not authenticate
// is not, and is recorded at warn.
func recordNoIdentity(ctx context.Context, chain [][]byte, err error) {
	level := slog.LevelWarn
	if len(chain) == 0 {
		level = slog.LevelInfo
	}
	slog.Log(ctx, level, "connection not authenticated",
		slog.String("error", err.Error()))
}

// authenticated is what one connection's handshake proved, written
// once by [Service.Authenticate] succeeding and read by every request
// served over that connection. A nil identity is a connection whose
// certificate did not authenticate, which is the state it is
// constructed in — there is no second field to disagree with it.
//
// One goroutine writes it, during a handshake that completes before any
// request on the connection is read, so nothing here is concurrent.
type authenticated struct {
	identity *Identity
}

type authenticatedKey struct{}

func authenticatedOf(ctx context.Context) (*authenticated, bool) {
	cell, ok := ctx.Value(authenticatedKey{}).(*authenticated)
	return cell, ok
}

// identify hands next the identity the request proved, where it proved
// one. A request presenting a token is answered by tokens, read for
// this request (ADR-0100). Any other request carries what
// [Service.Authenticate] returned during the handshake, so an identity
// reaching a handler is one whose chain was verified — nothing here
// reads the presented certificate a second time.
//
// A token that proves nothing leaves the request without an identity,
// as a certificate that does not authenticate leaves its connection,
// and the reason travels beside it for the handler's refusal to carry.
// An identity it hands next is noted in evidence first.
func identify(tokens Tokens, evidence *Evidence, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cell, ok := authenticatedOf(r.Context())
		certified := ok && cell.identity != nil
		token, presented := PresentedToken(r)
		switch {
		case presented && certified:
			refuse(w, r, next, errorx.New(errorx.KindUnauthenticated,
				"machine: a request presents a token or a certificate, not both"))
		case presented && len(r.Header.Values("Authorization")) > 1:
			refuse(w, r, next, errorx.New(errorx.KindUnauthenticated,
				"machine: a request presenting a token carries one Authorization header"))
		case presented:
			bearer, err := tokens.Resolve(r.Context(), token)
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
			identity := Identity{grn: bearer.Subject, method: MethodToken, serial: bearer.Serial,
				notBefore: bearer.IssuedAt, notAfter: bearer.ExpiresAt}
			evidence.Note(r.Context(), Sighting{Caller: bearer.Subject, Path: PathToken})
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)))
		case certified:
			evidence.Note(r.Context(), Sighting{Caller: cell.identity.GRN(), Path: PathCertificate})
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, *cell.identity)))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// PresentedToken returns the token a request presents in any of its
// Authorization headers, under [TokenScheme] or its alias
// [BearerScheme] (ADR-0102), and whether it presents one. Another
// scheme is not a token: the execution and operation headers share
// Authorization on routes a certificate authenticates.
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
// beside a certificate. A request that presented none answers nil, and
// its absence of identity is the handler's to decide on.
func RefusalOf(r *http.Request) error {
	err, ok := r.Context().Value(refusalKey{}).(error)
	if !ok {
		return nil
	}
	return err
}

// IdentityOf returns the machine identity the request authenticated
// as. The comma-ok is the whole of the distinction this listener
// makes: a request that carries no identity presented no certificate
// this organization's authority signed, and what such a request may
// reach is the handler's to decide.
func IdentityOf(r *http.Request) (Identity, bool) {
	identity, ok := r.Context().Value(identityKey{}).(Identity)
	return identity, ok
}
