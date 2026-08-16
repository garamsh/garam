package machine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
)

// NewServer returns the machine-facing HTTP server: it terminates TLS
// with cert, requires a client certificate, and serves next only for a
// connection whose certificate authenticated (ADR-0014). The caller
// gives it an address and serves it.
//
// The chain is verified here rather than by the TLS stack. ClientCAs
// verifies against one pool, and which authority may sign is decided by
// the organization the certificate's own SAN names — a pool holding
// every organization's CA would accept a certificate against a root
// that is not its own, which is the tenancy ADR-0014 rests on the trust
// root. What TLS is left to enforce is that a certificate is presented
// at all.
func NewServer(s Service, cert tls.Certificate, next http.Handler) *http.Server {
	// TLS 1.3 only: both ends of this surface are garam's, so neither
	// needs an older one.
	base := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
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
// reads it by the same rule, so what reaches a handler is the GRN
// [Service.Authenticate] returned. The refusal is what a request that
// reached here any other way gets instead.
func identify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "no client certificate", http.StatusUnauthorized)
			return
		}
		subject, err := subjectOf(r.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(w, "certificate does not authenticate", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, Identity{grn: subject})))
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

// IdentityHandler answers with the GRN the connection authenticated
// as. It is the whole of what this listener serves today: the message
// endpoints are a change of their own, and until one exists a freshly
// provisioned agent has nothing to call that tells a certificate which
// works from one that was never tried.
func IdentityHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := IdentityOf(r)
		if !ok {
			http.Error(w, "no machine identity", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// A failed write after the header cannot be reported to the
		// caller.
		_ = json.NewEncoder(w).Encode(struct {
			GRN string `json:"grn"`
		}{GRN: identity.GRN().String()})
	})
}
