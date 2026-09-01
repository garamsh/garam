package machine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
)

// NewServer returns the machine-facing HTTP server: it terminates TLS
// with the certificate that seam answers with, requires a client
// certificate, and serves next only for a connection whose certificate
// authenticated (ADR-0014). The chain is verified in next and not by
// the TLS stack, because which authority may sign is decided by the
// organization the certificate's own SAN names; what TLS is left to
// enforce is that a certificate is presented at all.
func NewServer(s Service, certificate ServerCertificate, next http.Handler) *http.Server {
	// TLS 1.3 only: both ends of this surface are garam's, so neither
	// needs an older one.
	base := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAnyClientCert,
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
	// connection that needed it instead of outliving it. Cloning only
	// then also carries what serving the config added to it, the ALPN
	// protocols among them.
	base.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		ctx := hello.Context()
		cfg := base.Clone()
		cfg.GetConfigForClient = nil
		cfg.VerifyPeerCertificate = func(chain [][]byte, _ [][]*x509.Certificate) error {
			_, err := s.Authenticate(ctx, chain)
			return err
		}
		return cfg, nil
	}
	return &http.Server{Handler: identify(next), TLSConfig: base}
}

// identify hands next the identity the connection authenticated as.
// The certificate it reads is the one the handshake verified, and it
// reads it by the same rule, so what reaches a handler is the identity
// [Service.Authenticate] returned. The refusal is what a request that
// reached here any other way gets instead.
func identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "no client certificate", http.StatusUnauthorized)
			return
		}
		identity, err := identityOf(r.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(w, "certificate does not authenticate", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)))
	})
}

type identityKey struct{}

// IdentityOf returns the machine identity the request authenticated
// as. Every request a server from [NewServer] serves carries one; the
// comma-ok is what a request served any other way reads instead.
func IdentityOf(r *http.Request) (Identity, bool) {
	identity, ok := r.Context().Value(identityKey{}).(Identity)
	return identity, ok
}
