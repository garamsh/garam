package machine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
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
// an operator enrolling with a token (ADR-0063) — and it costs nothing
// elsewhere: what an admitted caller may do is decided by the identity
// it holds, and a request that carries none reaches no handler that
// asks for one.
func NewServer(s Service, certificate ServerCertificate, next http.Handler) *http.Server {
	// TLS 1.3 only: both ends of this surface are garam's, so neither
	// needs an older one.
	base := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.RequestClientCert,
	}
	// Asked per handshake rather than frozen into Certificates, so a
	// replaced certificate is presented on the next connection with
	// nothing restarted (ADR-0040). The client half below is untouched
	// by it: TLS 1.3 sends the server's certificate before the client
	// sends one, so nothing here reads what the caller presented.
	base.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return certificate.Get()
	}
	// Per connection, because the handshake's context reaches
	// VerifyPeerCertificate no other way: a lookup then ends with the
	// connection that needed it instead of outliving it, and the cell
	// the outcome is written to is the one this connection's requests
	// read. Cloning only then also carries what serving the config
	// added to it, the ALPN protocols among them.
	base.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		ctx := hello.Context()
		cfg := base.Clone()
		cfg.GetConfigForClient = nil
		cfg.VerifyPeerCertificate = func(chain [][]byte, _ [][]*x509.Certificate) error {
			identity, err := s.Authenticate(ctx, chain)
			if err != nil {
				// The handshake completes and the connection stays
				// anonymous. Refusing here instead would take the
				// enrollment route down with every caller that has no
				// certificate yet, which is all of them.
				return nil
			}
			if cell, ok := authenticatedOf(ctx); ok {
				cell.identity, cell.ok = identity, true
			}
			return nil
		}
		return cfg, nil
	}
	return &http.Server{
		Handler:   identify(next),
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

// authenticated is what one connection's handshake proved, written
// once by [Service.Authenticate] succeeding and read by every request
// served over that connection. A connection whose certificate did not
// authenticate leaves it as it was constructed.
//
// One goroutine writes it, during a handshake that completes before any
// request on the connection is read, so nothing here is concurrent.
type authenticated struct {
	identity Identity
	ok       bool
}

type authenticatedKey struct{}

func authenticatedOf(ctx context.Context) (*authenticated, bool) {
	cell, ok := ctx.Value(authenticatedKey{}).(*authenticated)
	return cell, ok
}

// identify hands next the identity the connection authenticated as,
// where it authenticated as one. What it passes on is what
// [Service.Authenticate] returned during the handshake, so an identity
// reaching a handler is one whose chain was verified — nothing here
// reads the presented certificate a second time.
func identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cell, ok := authenticatedOf(r.Context())
		if !ok || !cell.ok {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, cell.identity)))
	})
}

type identityKey struct{}

// IdentityOf returns the machine identity the request authenticated
// as. The comma-ok is the whole of the distinction this listener
// makes: a request that carries no identity presented no certificate
// this organization's authority signed, and what such a request may
// reach is the handler's to decide.
func IdentityOf(r *http.Request) (Identity, bool) {
	identity, ok := r.Context().Value(identityKey{}).(Identity)
	return identity, ok
}
