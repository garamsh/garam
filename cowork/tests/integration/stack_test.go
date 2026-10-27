// Package integration_test drives cowork's wired server, as
// cli.NewHandler assembles it, over real Postgres. The platform behind
// it is substituted: a server answering `me` and `checkPermissions` as
// the platform's API does, under a policy each test sets.
package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/cli"
)

// migrationsDir is cowork/migrations, found from this file.
func migrationsDir(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

var migrateOnce sync.Once

// pool connects to COWORK_TEST_DATABASE_URL, migrates it once per run,
// and empties every table, so each test starts from none.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("COWORK_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("COWORK_TEST_DATABASE_URL is unset — run the suite through `make integration`")
	}
	var migrateErr error
	migrateOnce.Do(func() { migrateErr = cli.Migrate(url, migrationsDir(t)) })
	require.NoError(t, migrateErr)
	p, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	_, err = p.Exec(context.Background(), "TRUNCATE pages, channels CASCADE")
	require.NoError(t, err)
	return p
}

// fakePlatform answers for four people: ada, bob and carol, members of
// acme, and eve, a member of none. A session cookie's value, or a
// bearer, is the person's name. It allows every check a member asks
// except those deny names, as `action` or `action@page`, for everyone,
// or prefixed with a name and a space, for that person alone.
type fakePlatform struct {
	mu   sync.Mutex
	deny map[string]bool
}

func (f *fakePlatform) denyCheck(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deny[key] = true
}

func (f *fakePlatform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	who, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		if c, err := r.Cookie("garam_session"); err == nil {
			who = c.Value
		}
	}
	if who != "ada" && who != "bob" && who != "carol" && who != "eve" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/orgs" && who != "eve":
		_ = json.NewEncoder(w).Encode([]map[string]string{{"grn": "grn:root:default:org:acme", "name": "Acme"}})
	case r.URL.Path == "/orgs":
		_ = json.NewEncoder(w).Encode([]map[string]string{})
	case r.URL.Path == "/me":
		_ = json.NewEncoder(w).Encode(map[string]string{"grn": "grn:root:default:user:" + who, "nickname": who})
	case r.URL.Path == "/orgs/acme/permission-checks" && who != "eve":
		var body struct {
			Checks []struct {
				Action   string `json:"action"`
				Resource *struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"resource"`
			} `json:"checks"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		results := make([]map[string]string, len(body.Checks))
		for i, ch := range body.Checks {
			key := ch.Action
			if ch.Resource != nil {
				key += "@" + ch.Resource.ID
			}
			decision := "allow"
			if f.deny[key] || f.deny[who+" "+key] {
				decision = "deny"
			}
			results[i] = map[string]string{"decision": decision}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	case strings.HasPrefix(r.URL.Path, "/orgs/"):
		w.WriteHeader(http.StatusForbidden)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// stack is cowork's server over real Postgres and the fake platform.
type stack struct {
	url      string
	pool     *pgxpool.Pool
	platform *fakePlatform
}

func newStack(t *testing.T) *stack {
	t.Helper()
	p := pool(t)
	fake := &fakePlatform{deny: map[string]bool{}}
	platformSrv := httptest.NewServer(fake)
	t.Cleanup(platformSrv.Close)
	h, err := cli.NewHandler(cli.Config{PlatformURL: platformSrv.URL}, p)
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &stack{url: srv.URL, pool: p, platform: fake}
}

// call makes one request as who and answers the status and body.
func (s *stack) call(t *testing.T, who, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.url+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "garam_session", Value: who})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// createPage creates a page in acme as ada and answers its ID.
func (s *stack) createPage(t *testing.T, title string) string {
	t.Helper()
	status, body := s.call(t, "ada", http.MethodPost, "/orgs/acme/pages", `{"title":"`+title+`"}`)
	require.Equal(t, http.StatusCreated, status, body)
	var p struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &p))
	return p.ID
}
