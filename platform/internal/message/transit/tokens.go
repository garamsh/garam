package transit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/keyfile"
)

// DefaultAuthMount is the JWT auth method's mount when the deployment
// names none.
const DefaultAuthMount = "jwt"

// renewAfter is the share of a token's lease after which the next call
// renews it, so a token in use is renewed before it lapses.
const renewAfter = 2.0 / 3.0

// tokenSource gives each call the OpenBao token it presents.
type tokenSource interface {
	// Token answers the token to present, which may take a login or a
	// renewal. Its errors name op and carry the kind a call answers
	// with.
	Token(ctx context.Context, op string) (string, error)
	// Refused records that OpenBao answered 403 to token, and reports
	// whether another token may be tried: only where token had served
	// an earlier call, so a policy that denies a fresh login is not
	// retried.
	Refused(token string) bool
}

// fileTokens reads the token from a file the deployment keeps current,
// on every call.
type fileTokens struct{ path string }

func (f fileTokens) Token(_ context.Context, op string) (string, error) {
	raw, err := keyfile.Read(f.path)
	if err != nil {
		return "", errorx.Wrap(err, "transit: "+op+": token file", errorx.KindUnavailable)
	}
	return strings.TrimSpace(string(raw)), nil
}

func (fileTokens) Refused(string) bool { return false }

// loginTokens holds an OpenBao token obtained by logging in to a JWT
// auth method with the workload identity in a file. The token lives in
// memory alone; no goroutine renews it, the next call does.
type loginTokens struct {
	client *http.Client
	base   *url.URL
	mount  string
	role   string
	file   string
	// now is the clock the lease is read against.
	now func() time.Time

	mu        sync.Mutex
	token     string
	renewable bool
	renewAt   time.Time
	expires   time.Time
	uses      int
}

// grant is the auth block OpenBao answers a login or a renewal with.
type grant struct {
	Auth struct {
		ClientToken   string `json:"client_token"`
		LeaseDuration int    `json:"lease_duration"`
		Renewable     bool   `json:"renewable"`
	} `json:"auth"`
}

func (l *loginTokens) Token(ctx context.Context, _ string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.token != "" && now.Before(l.renewAt) {
		l.uses++
		return l.token, nil
	}
	if l.token != "" && l.renewable {
		err := l.exchange(ctx, "renew", "auth/token/renew-self", l.token, nil)
		if err == nil {
			l.uses++
			return l.token, nil
		}
		// An OpenBao that could not answer leaves a token that has not
		// lapsed usable; one that refused has dropped it.
		if errorx.IsKind(err, errorx.KindUnavailable) && now.Before(l.expires) {
			l.uses++
			return l.token, nil
		}
	}
	l.token = ""
	if err := l.login(ctx); err != nil {
		return "", err
	}
	l.uses = 1
	return l.token, nil
}

func (l *loginTokens) Refused(token string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if token != l.token || l.uses < 2 {
		return false
	}
	l.token = ""
	return true
}

// login exchanges the identity file's current contents for a token.
// The file is read each time, so an identity renewed in place is the
// one presented.
func (l *loginTokens) login(ctx context.Context) error {
	raw, err := keyfile.Read(l.file)
	if err != nil {
		return errorx.Wrap(err, "transit: login: identity file", errorx.KindUnavailable)
	}
	identity := strings.TrimSpace(string(raw))
	if identity == "" {
		return errorx.New(errorx.KindUnavailable, "transit: login: identity file is empty")
	}
	return l.exchange(ctx, "login", "auth/"+l.mount+"/login", "",
		map[string]string{"role": l.role, "jwt": identity})
}

// exchange posts to path, under token where there is one, and keeps the
// token and lease the answer carries. Neither the identity nor a token
// is part of an error.
func (l *loginTokens) exchange(ctx context.Context, op, path, token string, body any) error {
	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return errorx.Wrap(err, "transit: "+op, errorx.KindInternal)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base.JoinPath("v1", path).String(), payload)
	if err != nil {
		return errorx.Wrap(err, "transit: "+op, errorx.KindInternal)
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return errorx.Wrap(err, "transit: "+op+": unreachable", errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return errorx.Wrap(err, "transit: "+op+": answer", errorx.KindUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		return refusal(op, resp.StatusCode, raw)
	}
	var g grant
	if err := json.Unmarshal(raw, &g); err != nil || g.Auth.LeaseDuration <= 0 ||
		(token == "" && g.Auth.ClientToken == "") {
		return errorx.New(errorx.KindInternal, "transit: "+op+": answer carries no token lease")
	}
	lease := time.Duration(g.Auth.LeaseDuration) * time.Second
	now := l.now()
	if token == "" {
		l.token = g.Auth.ClientToken
	}
	l.renewable = g.Auth.Renewable
	l.renewAt = now.Add(time.Duration(float64(lease) * renewAfter))
	l.expires = now.Add(lease)
	return nil
}
