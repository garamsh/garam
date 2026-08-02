package authn

import (
	"context"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/garamsh/garam/internal/errorx"
)

// GoogleConfig configures the Google provider. Issuer defaults to
// https://accounts.google.com and exists for tests.
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Issuer       string
}

// googleIssuer is the default OIDC issuer for Google accounts.
const googleIssuer = "https://accounts.google.com"

type googleProvider struct {
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewGoogleProvider constructs the Google Provider, discovering OIDC
// metadata from the issuer.
func NewGoogleProvider(ctx context.Context, cfg GoogleConfig) (Provider, error) {
	issuer := cfg.Issuer
	if issuer == "" {
		issuer = googleIssuer
	}
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, errorx.Wrap(err, "oidc discovery", errorx.KindUnavailable)
	}
	return &googleProvider{
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier: p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// Name returns the provider key used in API paths.
func (g *googleProvider) Name() string { return "google" }

// AuthCodeURL returns the Google authorization URL with the PKCE S256
// challenge derived from the verifier.
func (g *googleProvider) AuthCodeURL(state, verifier string) string {
	return g.oauth2.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// Exchange trades the authorization code for tokens and verifies the
// ID token, returning the asserted Identity.
func (g *googleProvider) Exchange(ctx context.Context, code, verifier string) (*Identity, error) {
	token, err := g.oauth2.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, errorx.Wrap(err, "token exchange", errorx.KindUnauthenticated)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errorx.New(errorx.KindUnauthenticated, "missing id_token")
	}
	idToken, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, errorx.Wrap(err, "id token verify", errorx.KindUnauthenticated)
	}
	var claims struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, errorx.Wrap(err, "id token claims", errorx.KindUnauthenticated)
	}
	nickname := claims.Name
	if nickname == "" {
		nickname = claims.Email
	}
	if nickname == "" {
		nickname = idToken.Subject
	}
	return &Identity{Subject: idToken.Subject, Nickname: nickname}, nil
}
