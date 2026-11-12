// Package policytest stands in for the policy service and the
// platform's machine token endpoint, in process, for the tests of
// cowork's side of them. The service it fakes applies the rule of
// policy/README.md §Callers to a caller bound to an organization; it
// is not the service, and what it decides is what a test tells it to.
package policytest

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
)

// Key writes a fresh RSA deployment key to a file only its owner can
// reach, and answers the file and the key's public half.
func Key(t *testing.T) (string, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "deployment.key")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, &key.PublicKey
}

// Thumbprint is the RFC 7638 SHA-256 thumbprint of pub, base64url.
func Thumbprint(pub *rsa.PublicKey) string {
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	sum := sha256.Sum256([]byte(`{"e":"` + e + `","kty":"RSA","n":"` + n + `"}`))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Exchange is one request the token endpoint accepted.
type Exchange struct {
	Org    string
	Form   map[string]string
	Header map[string]any
	Claims map[string]any
}

// Endpoint is a machine token endpoint that accepts assertions signed
// by one key, as the platform's does (ADR-0129), and issues a token
// per organization.
type Endpoint struct {
	// URL is the endpoint's address, which an assertion's `aud` names.
	URL string
	// ExpiresIn is the lifetime it answers, in seconds; 300 if zero.
	ExpiresIn int64

	pub *rsa.PublicKey

	mu        sync.Mutex
	exchanges []Exchange
	issued    map[string]string
	count     int
}

// NewEndpoint serves an endpoint that trusts pub.
func NewEndpoint(t *testing.T, pub *rsa.PublicKey) *Endpoint {
	t.Helper()
	e := &Endpoint{pub: pub, issued: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(srv.Close)
	e.URL = srv.URL + "/oauth/token"
	return e
}

// Exchanges are the requests accepted so far.
func (e *Endpoint) Exchanges() []Exchange {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Exchange(nil), e.exchanges...)
}

// Org is the organization a token this endpoint issued is for.
func (e *Endpoint) Org(token string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	org, ok := e.issued[token]
	return org, ok
}

var jtiPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22,128}$`)

func (e *Endpoint) serve(w http.ResponseWriter, r *http.Request) {
	refuse := func(msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "error_description": msg})
	}
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		refuse("not a form POST")
		return
	}
	if r.Header.Get("Authorization") != "" {
		refuse("an Authorization header")
		return
	}
	if err := r.ParseForm(); err != nil {
		refuse("form")
		return
	}
	form := map[string]string{}
	for k := range r.PostForm {
		form[k] = r.PostForm.Get(k)
	}
	if form["grant_type"] != "client_credentials" ||
		form["client_assertion_type"] != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" ||
		form["audience"] != "policy" || len(form) != 5 {
		refuse("parameters")
		return
	}
	header, claims, err := e.verify(form["client_assertion"])
	if err != nil {
		refuse(err.Error())
		return
	}
	id, err := grn.Parse(form["client_id"])
	if err != nil || id.Service != "iam" || id.ServiceType != "service" || id.Identifier != "cowork" || id.Ancestors != "" {
		refuse("client_id")
		return
	}
	if claims["iss"] != form["client_id"] || claims["sub"] != form["client_id"] || claims["aud"] != e.URL {
		refuse("claims")
		return
	}
	e.mu.Lock()
	e.count++
	token := fmt.Sprintf("token-%s-%d", id.Organization, e.count)
	e.issued[token] = id.Organization
	e.exchanges = append(e.exchanges, Exchange{Org: id.Organization, Form: form, Header: header, Claims: claims})
	expires := e.ExpiresIn
	e.mu.Unlock()
	if expires == 0 {
		expires = 300
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": expires})
}

func (e *Endpoint) verify(jwt string) (map[string]any, map[string]any, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return nil, nil, fmt.Errorf("not a JWT")
	}
	var header, claims map[string]any
	for i, dst := range []*map[string]any{&header, &claims} {
		raw, err := base64.RawURLEncoding.DecodeString(parts[i])
		if err != nil || json.Unmarshal(raw, dst) != nil {
			return nil, nil, fmt.Errorf("not a JWT")
		}
	}
	if header["alg"] != "RS256" || header["kid"] != Thumbprint(e.pub) || len(header) != 3 {
		return nil, nil, fmt.Errorf("header")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, nil, fmt.Errorf("signature")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(e.pub, crypto.SHA256, digest[:], sig); err != nil {
		return nil, nil, fmt.Errorf("signature")
	}
	iat, _ := claims["iat"].(float64)
	exp, _ := claims["exp"].(float64)
	if exp-iat <= 0 || exp-iat > 60 {
		return nil, nil, fmt.Errorf("lifetime")
	}
	if jti, _ := claims["jti"].(string); !jtiPattern.MatchString(jti) {
		return nil, nil, fmt.Errorf("jti")
	}
	return header, claims, nil
}

// Evaluated is one Evaluations call the service received.
type Evaluated struct {
	Org     string
	Token   string
	Request *policyv1.EvaluationsRequest
}

// Applied is one ApplyEntityChanges call the service received.
type Applied struct {
	Org     string
	Token   string
	Request *policyv1.ApplyEntityChangesRequest
}

// Server is a policy service that knows a caller by the tokens an
// Endpoint issued, and applies the rule of policy/README.md §Callers.
type Server struct {
	// Addr and CAFile are what cowork is configured with.
	Addr   string
	CAFile string

	// Decide answers one evaluation, its defaults filled in, asked as
	// org's cowork. A nil Decide denies.
	Decide func(org string, e *policyv1.Evaluation) bool
	// Refuse gives the reason the service refuses one entry for its
	// own fault, or "" to accept it.
	Refuse func(c *policyv1.EntityChange) string

	endpoint *Endpoint
	mu       sync.Mutex
	asked    []Evaluated
	applied  []Applied
}

// Asked are the Evaluations calls received, in order.
func (s *Server) Asked() []Evaluated {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Evaluated(nil), s.asked...)
}

// Applied are the ApplyEntityChanges calls received, in order,
// refused ones included.
func (s *Server) Applied() []Applied {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Applied(nil), s.applied...)
}

// Serve starts a policy service over TLS on the loopback, with a CA
// generated for it, trusting the tokens endpoint issued.
func Serve(t *testing.T, endpoint *Endpoint) *Server {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "policytest CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "policytest"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Addr: lis.Addr().String(), CAFile: caFile, endpoint: endpoint}
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewServerTLSFromCert(&tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey})),
		grpc.UnaryInterceptor(s.authenticate),
	)
	policyv1.RegisterPolicyServiceServer(srv, &service{Server: s})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return s
}

type callerKey struct{}

type caller struct{ org, token string }

func (s *Server) authenticate(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return nil, status.Error(codes.Unauthenticated, "no bearer token")
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	org, ok := s.endpoint.Org(token)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unknown token")
	}
	return next(context.WithValue(ctx, callerKey{}, caller{org: org, token: token}), req)
}

type service struct {
	*Server
	policyv1.UnimplementedPolicyServiceServer
}

// owned are the types cowork writes (policy/README.md §Callers).
var owned = map[string]bool{
	"Cowork::Channel": true, "Cowork::Page": true, "Cowork::Task": true, "Cowork::Artifact": true,
}

func (s *service) Evaluations(ctx context.Context, req *policyv1.EvaluationsRequest) (*policyv1.EvaluationsResponse, error) {
	c := ctx.Value(callerKey{}).(caller)
	s.mu.Lock()
	s.asked = append(s.asked, Evaluated{Org: c.org, Token: c.token, Request: req})
	s.mu.Unlock()
	resp := &policyv1.EvaluationsResponse{}
	for _, e := range req.GetEvaluations() {
		filled := &policyv1.Evaluation{Subject: e.GetSubject(), Action: e.GetAction(), Resource: e.GetResource()}
		if filled.Subject == nil {
			filled.Subject = req.GetSubject()
		}
		if filled.Action == nil {
			filled.Action = req.GetAction()
		}
		if filled.Resource == nil {
			filled.Resource = req.GetResource()
		}
		if !strings.HasPrefix(filled.GetAction().GetName(), "cowork:") ||
			!strings.HasPrefix(filled.GetResource().GetId(), "grn:"+c.org+"/") && filled.GetResource().GetId() != grn.Org(c.org).String() {
			return nil, status.Error(codes.PermissionDenied, "a cowork asks only cowork: actions on resources of its organization")
		}
		allow := s.Decide != nil && s.Decide(c.org, filled)
		resp.Evaluations = append(resp.Evaluations, &policyv1.EvaluateResponse{Decision: allow})
	}
	return resp, nil
}

func (s *service) ApplyEntityChanges(ctx context.Context, req *policyv1.ApplyEntityChangesRequest) (*policyv1.ApplyEntityChangesResponse, error) {
	c := ctx.Value(callerKey{}).(caller)
	s.mu.Lock()
	s.applied = append(s.applied, Applied{Org: c.org, Token: c.token, Request: req})
	s.mu.Unlock()
	if len(req.GetChanges()) > 1000 {
		return nil, status.Error(codes.InvalidArgument, "more than 1000 changes")
	}
	org := grn.Org(c.org).String()
	for _, ch := range req.GetChanges() {
		if !owned[ch.GetEntity().GetType()] {
			return nil, status.Error(codes.PermissionDenied, "a type cowork does not own: "+ch.GetEntity().GetType())
		}
		if !strings.HasPrefix(ch.GetEntity().GetId(), "grn:"+c.org+"/") {
			return nil, status.Error(codes.PermissionDenied, "an entity outside the organization")
		}
		if up := ch.GetUpsert(); up != nil {
			if up.GetOrg().GetType() != "Iam::Org" || up.GetOrg().GetId() != org {
				return nil, status.Error(codes.PermissionDenied, "an org that is not the organization's own")
			}
			for _, p := range up.GetParents() {
				if p.GetId() != org {
					return nil, status.Error(codes.PermissionDenied, "a parent outside the organization")
				}
			}
		}
	}
	resp := &policyv1.ApplyEntityChangesResponse{}
	for i, ch := range req.GetChanges() {
		if s.Refuse != nil {
			if reason := s.Refuse(ch); reason != "" {
				resp.Refusals = append(resp.Refusals, &policyv1.EntityChangeRefusal{Index: uint32(i), Error: reason})
				continue
			}
		}
		resp.Written++
	}
	return resp, nil
}
