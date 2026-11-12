// Package identity is garam as an OIDC issuer (ADR-0141): the
// discovery document and key set it publishes, the RFC 8693 exchange
// of a CLI session's access token for a signed token, and the RFC 7662
// introspection a service asks of one. It holds no state of its own.
// What a token vouches for is the session row it names by `sid`, so
// revoking the session is deleting the row, and the next exchange is
// refused and the next introspection answers inactive.
package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/signer"
)

// The audiences a token may be issued for: a static list held here and
// not in configuration or a table (ADR-0141 item 4).
const (
	AudiencePolicy  = "policy"
	AudienceCodehub = "codehub"
)

// Audiences is the whole list, in the order the documents name it.
var Audiences = []string{AudiencePolicy, AudienceCodehub}

// The RFC 8693 identifiers the exchange speaks.
const (
	GrantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	// TokenTypeAccessToken names the subject token, and is also what the
	// response's issued_token_type says (ADR-0141 item 3).
	TokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"
	TokenTypeJWT         = "urn:ietf:params:oauth:token-type:jwt"
)

// Paths the issuer's operations are served at, beneath the issuer.
const (
	JWKSPath  = "/oauth/jwks"
	TokenPath = "/oauth/token"
)

// What a refusal is. Each is a sentinel the transport translates to
// RFC 6749's code, and none names which part of the subject failed.
var (
	// ErrInvalidRequest is a parameter missing, repeated or of a value
	// the exchange does not take.
	ErrInvalidRequest = errorx.New(errorx.KindInvalidArgument, "identity: invalid request")
	// ErrInvalidTarget is an audience outside [Audiences].
	ErrInvalidTarget = errorx.New(errorx.KindInvalidArgument, "identity: audience is not one the issuer serves")
	// ErrInvalidGrant is a subject token that is not the access token of
	// a CLI session with life left in it: a refresh token, an ID token,
	// a machine token, a browser session, or a session revoked or
	// expired.
	ErrInvalidGrant = errorx.New(errorx.KindUnauthenticated, "identity: subject token is not a live session's access token")
)

// Sessions is what the issuer reads sessions through.
type Sessions interface {
	// Get answers the live session whose access token is token, or
	// authn.ErrSessionNotFound.
	Get(ctx context.Context, token string) (*authn.Session, error)
	// GetByID answers the live session whose row id is id, or
	// authn.ErrSessionNotFound.
	GetByID(ctx context.Context, id string) (*authn.Session, error)
}

// ParseIssuer returns raw where it is an issuer identity of ADR-0141
// item 7: https, a lowercase host, no empty port and no user
// information, the path /api, no trailing slash, query or fragment. The
// identity is compared byte for byte wherever it is read, so only the
// one spelling is accepted and none is normalized.
func ParseIssuer(raw string) (string, error) {
	invalid := errorx.New(errorx.KindInvalidArgument,
		"issuer: https, a lowercase host with no empty port, path /api, no trailing slash")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Host != strings.ToLower(u.Host) || strings.HasSuffix(u.Host, ":") ||
		u.Path != "/api" || u.RawQuery != "" || u.Fragment != "" ||
		raw != "https://"+u.Host+"/api" {
		return "", invalid
	}
	return raw, nil
}

// Issuer exchanges and introspects. It is built once with the one key
// the platform signs with, and is safe for concurrent use.
type Issuer struct {
	issuer    string
	key       *signer.Key
	sessions  Sessions
	now       func() time.Time
	discovery []byte
	jwks      []byte
}

// NewIssuer builds the issuer named by issuer, which [ParseIssuer]
// read. A nil now takes time.Now.
func NewIssuer(issuer string, key *signer.Key, sessions Sessions, now func() time.Time) (*Issuer, error) {
	jwks, err := key.JWKS()
	if err != nil {
		return nil, err
	}
	discovery, err := json.Marshal(discoveryDocument{
		Issuer:                           issuer,
		JWKSURI:                          issuer + JWKSPath,
		TokenEndpoint:                    issuer + TokenPath,
		GrantTypesSupported:              []string{GrantTypeTokenExchange},
		ResponseTypesSupported:           []string{},
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: []string{"RS256"},
		TokenEndpointAuthMethods:         []string{"none"},
	})
	if err != nil {
		return nil, errorx.Wrap(err, "identity: encode discovery document", errorx.KindInternal)
	}
	if now == nil {
		now = time.Now
	}
	return &Issuer{issuer: issuer, key: key, sessions: sessions, now: now, discovery: discovery, jwks: jwks}, nil
}

// discoveryDocument is the body served at the discovery path.
type discoveryDocument struct {
	Issuer                           string   `json:"issuer"`
	JWKSURI                          string   `json:"jwks_uri"`
	TokenEndpoint                    string   `json:"token_endpoint"`
	GrantTypesSupported              []string `json:"grant_types_supported"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethods         []string `json:"token_endpoint_auth_methods_supported"`
}

// Discovery is the discovery document, ready to serve.
// Identifier is the issuer identifier, GARAM_IDENTITY_ISSUER.
func (i *Issuer) Identifier() string { return i.issuer }

func (i *Issuer) Discovery() []byte { return i.discovery }

// JWKS is the key set, ready to serve.
func (i *Issuer) JWKS() []byte { return i.jwks }

// ExchangeRequest is an RFC 8693 request as the exchange reads it.
type ExchangeRequest struct {
	SubjectToken       string
	SubjectTokenType   string
	Audience           string
	RequestedTokenType string
}

// Exchanged is the exchange's answer.
type Exchanged struct {
	Token string
	// ExpiresIn is the token's lifetime in whole seconds.
	ExpiresIn int
}

// Exchange answers a token for the CLI session whose access token the
// request presents. Its lifetime is the shorter of [signer.MaxLifetime]
// and what the session has left (ADR-0141 item 5). The session is read
// by its access token, so a token that is not one, a refresh token, an
// ID token or a machine token, finds no row and is refused with the
// rest as [ErrInvalidGrant]; a browser session is refused the same way,
// because only a CLI's access token is a subject here.
func (i *Issuer) Exchange(ctx context.Context, req ExchangeRequest) (Exchanged, error) {
	switch {
	case req.SubjectToken == "":
		return Exchanged{}, ErrInvalidRequest
	case req.SubjectTokenType != TokenTypeAccessToken:
		return Exchanged{}, ErrInvalidRequest
	case req.RequestedTokenType != "" && req.RequestedTokenType != TokenTypeJWT:
		return Exchanged{}, ErrInvalidRequest
	case !knownAudience(req.Audience):
		return Exchanged{}, ErrInvalidTarget
	}
	session, err := i.sessions.Get(ctx, req.SubjectToken)
	switch {
	case errors.Is(err, authn.ErrSessionNotFound):
		return Exchanged{}, ErrInvalidGrant
	case err != nil:
		return Exchanged{}, err
	case session.Kind != authn.SessionCLI:
		return Exchanged{}, ErrInvalidGrant
	}
	now := i.now()
	lifetime := min(signer.MaxLifetime, session.ExpiresAt.Sub(now)).Truncate(time.Second)
	if lifetime < time.Second {
		return Exchanged{}, ErrInvalidGrant
	}
	jti, err := newJTI()
	if err != nil {
		return Exchanged{}, err
	}
	token, err := i.key.Sign(signer.Claims{
		Issuer:    i.issuer,
		Subject:   session.User.String(),
		Audience:  req.Audience,
		IssuedAt:  now,
		Lifetime:  lifetime,
		JTI:       jti,
		SessionID: session.ID,
	})
	if err != nil {
		return Exchanged{}, err
	}
	return Exchanged{Token: token, ExpiresIn: int(lifetime / time.Second)}, nil
}

// IssueCaller answers a token for the service principal subject to
// present to the policy service: this issuer's, `sub` the principal's
// GRN, `aud` the policy service, and no session behind it, which is
// what tells it from a CLI user's token. Its lifetime is
// [signer.MaxLifetime], and the caller has proved itself before this is
// asked (ADR-0129 item 3).
func (i *Issuer) IssueCaller(subject grn.GRN) (Exchanged, error) {
	jti, err := newJTI()
	if err != nil {
		return Exchanged{}, err
	}
	token, err := i.key.Sign(signer.Claims{
		Issuer:   i.issuer,
		Subject:  subject.String(),
		Audience: AudiencePolicy,
		IssuedAt: i.now(),
		Lifetime: signer.MaxLifetime,
		JTI:      jti,
	})
	if err != nil {
		return Exchanged{}, err
	}
	return Exchanged{Token: token, ExpiresIn: int(signer.MaxLifetime / time.Second)}, nil
}

func knownAudience(audience string) bool {
	for _, known := range Audiences {
		if audience == known {
			return true
		}
	}
	return false
}

// newJTI returns a random identifier for one token.
func newJTI() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "identity: jti", errorx.KindInternal)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// Introspect answers the claims of token where it is active for
// audience, and ok false where it is not. A token is active when this
// issuer signed it, it names audience, it has not expired, and its
// `sid` names a session row that is still live and belongs to its
// `sub`. Every way of not being active is the same answer, so it says
// nothing about which it was, and a token of another audience is one
// of them: a service learns nothing about tokens that are not its own
// (ADR-0141 item 3). An error is a store that could not be read.
func (i *Issuer) Introspect(ctx context.Context, audience, token string) (signer.Claims, bool, error) {
	claims, err := i.key.Verify(token, i.now())
	if err != nil || claims.Issuer != i.issuer || claims.Audience != audience || claims.SessionID == "" {
		return signer.Claims{}, false, nil
	}
	session, err := i.sessions.GetByID(ctx, claims.SessionID)
	switch {
	case errors.Is(err, authn.ErrSessionNotFound):
		return signer.Claims{}, false, nil
	case err != nil:
		return signer.Claims{}, false, err
	case session.User.String() != claims.Subject:
		return signer.Claims{}, false, nil
	}
	return claims, true, nil
}
