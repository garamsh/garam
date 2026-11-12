package policy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

const (
	// Audience is what a caller token is asked for (ADR-0138).
	Audience = "policy"

	// expiryMargin is how long before a token's expiry it is asked
	// for again, so a call never starts on a token about to lapse.
	expiryMargin = 30 * time.Second

	// maxExpiresIn is the longest a token lives (ADR-0129).
	maxExpiresIn = 300 * time.Second

	exchangeTimeout = 10 * time.Second
	assertionType   = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
)

// Tokens fetches and caches cowork's caller token for each
// organization.
type Tokens struct {
	tokenURL string
	keyFile  string
	client   *http.Client
	now      func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewTokens answers a source of caller tokens from the endpoint at
// cfg.TokenURL, signing with the key in cfg.KeyFile and trusting the
// CA in cfg.CAFile besides the system roots. A nil now is time.Now.
func NewTokens(cfg Config, now func() time.Time) (*Tokens, error) {
	if cfg.TokenURL == "" || cfg.KeyFile == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: the token URL and deployment key file are required")
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, errorx.Wrap(err, "policy: read CA file", errorx.KindInvalidArgument)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errorx.New(errorx.KindInvalidArgument, "policy: CA file holds no certificate")
		}
	}
	if now == nil {
		now = time.Now
	}
	return &Tokens{
		tokenURL: cfg.TokenURL,
		keyFile:  cfg.KeyFile,
		client: &http.Client{
			Timeout:   exchangeTimeout,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
		now:     now,
		entries: map[string]*entry{},
	}, nil
}

// ClientID is the principal cowork presents as in org:
// grn:<org>/iam/service/cowork (ADR-0129).
func ClientID(org string) string {
	return grn.NewIn(org, "iam", "service", "cowork").String()
}

// Token answers cowork's caller token for org, asking the endpoint
// when it holds none that is still good.
func (t *Tokens) Token(ctx context.Context, org string) (string, error) {
	t.mu.Lock()
	e := t.entries[org]
	if e == nil {
		e = &entry{}
		t.entries[org] = e
	}
	t.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.token != "" && t.now().Before(e.expires) {
		return e.token, nil
	}
	token, lifetime, err := t.exchange(ctx, org)
	if err != nil {
		return "", err
	}
	e.token, e.expires = token, t.now().Add(lifetime-expiryMargin)
	return token, nil
}

// exchange asks the endpoint for one token, signing a fresh assertion
// with the key as the file holds it now.
func (t *Tokens) exchange(ctx context.Context, org string) (string, time.Duration, error) {
	raw, err := readKeyFile(t.keyFile)
	if err != nil {
		return "", 0, errorx.Wrap(err, "policy: deployment key", errorx.KindUnavailable)
	}
	key, err := parseKey(raw)
	if err != nil {
		return "", 0, errorx.Wrap(err, "policy: deployment key", errorx.KindUnavailable)
	}
	clientID := ClientID(org)
	signed, err := assertion(key, clientID, t.tokenURL, t.now())
	if err != nil {
		return "", 0, err
	}
	form := url.Values{
		"grant_type":            {"client_credentials"},
		"client_id":             {clientID},
		"client_assertion_type": {assertionType},
		"client_assertion":      {signed},
		"audience":              {Audience},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, errorx.Wrap(err, "policy: token request", errorx.KindInvalidArgument)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", 0, errorx.Wrap(err, "policy: token endpoint unreachable", errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", 0, errorx.Wrap(err, "policy: read token answer", errorx.KindUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, errorx.New(errorx.KindUnavailable, fmt.Sprintf("policy: token endpoint answered %d", resp.StatusCode))
	}
	var answer struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.AccessToken == "" || !strings.EqualFold(answer.TokenType, "Bearer") {
		return "", 0, errorx.New(errorx.KindUnavailable, "policy: token endpoint answered no bearer token")
	}
	lifetime := min(time.Duration(answer.ExpiresIn)*time.Second, maxExpiresIn)
	if lifetime <= expiryMargin {
		return "", 0, errorx.New(errorx.KindUnavailable, "policy: token endpoint answered a token about to expire")
	}
	return answer.AccessToken, lifetime, nil
}
