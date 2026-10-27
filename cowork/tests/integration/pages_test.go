package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/cli"
	"github.com/garamsh/garam/cowork/internal/collab"
)

type apiPage struct {
	ID        string `json:"id"`
	Grn       string `json:"grn"`
	Parent    string `json:"parent"`
	Title     string `json:"title"`
	Position  int    `json:"position"`
	CreatedBy string `json:"createdBy"`
}

func (s *stack) list(t *testing.T, who string) []apiPage {
	t.Helper()
	status, body := s.call(t, who, http.MethodGet, "/orgs/acme/pages", "")
	require.Equal(t, http.StatusOK, status, body)
	var out struct {
		Pages []apiPage `json:"pages"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	return out.Pages
}

// TestPages_BuildAndReshapeATree creates a page under another, renames
// it, moves it to the top at position 0 and deletes its old parent,
// reading the tree back after each step from Postgres.
func TestPages_BuildAndReshapeATree(t *testing.T) {
	s := newStack(t)
	parent := s.createPage(t, "parent")
	status, body := s.call(t, "bob", http.MethodPost, "/orgs/acme/pages", `{"title":"child","parent":"`+parent+`"}`)
	require.Equal(t, http.StatusCreated, status, body)
	var child apiPage
	require.NoError(t, json.Unmarshal([]byte(body), &child))
	assert.Equal(t, parent, child.Parent)
	assert.Equal(t, "grn:root:default:user:bob", child.CreatedBy)
	assert.Equal(t, "grn:acme:default:cowork-page:"+child.ID, child.Grn)

	status, body = s.call(t, "ada", http.MethodPatch, "/orgs/acme/pages/"+child.ID,
		`{"title":"renamed","parent":"","position":0}`)
	require.Equal(t, http.StatusOK, status, body)
	pages := s.list(t, "ada")
	require.Len(t, pages, 2)
	assert.Equal(t, "renamed", pages[0].Title)
	assert.Equal(t, []int{0, 1}, []int{pages[0].Position, pages[1].Position})

	status, _ = s.call(t, "ada", http.MethodDelete, "/orgs/acme/pages/"+parent, "")
	require.Equal(t, http.StatusNoContent, status)
	pages = s.list(t, "ada")
	require.Len(t, pages, 1)
	assert.Equal(t, 0, pages[0].Position)
}

// TestPages_DeleteTakesThePagesBeneathAndTheirContent deletes a page
// that has a child with content, and reads no page, update or
// snapshot of either back.
func TestPages_DeleteTakesThePagesBeneathAndTheirContent(t *testing.T) {
	s := newStack(t)
	parent := s.createPage(t, "parent")
	status, body := s.call(t, "ada", http.MethodPost, "/orgs/acme/pages", `{"title":"child","parent":"`+parent+`"}`)
	require.Equal(t, http.StatusCreated, status, body)
	var child apiPage
	require.NoError(t, json.Unmarshal([]byte(body), &child))
	e := newEditor(t, s, "ada", child.ID)
	e.connect()
	e.appendBlock(collab.Block{Type: "paragraph", Text: "content"})
	var rows int
	eventually(t, "the child's content is kept", func() bool {
		_ = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM page_updates WHERE page_id = $1", child.ID).Scan(&rows)
		return rows > 0
	})
	e.disconnect()

	status, _ = s.call(t, "ada", http.MethodDelete, "/orgs/acme/pages/"+parent, "")
	require.Equal(t, http.StatusNoContent, status)
	require.NoError(t, s.pool.QueryRow(context.Background(),
		"SELECT (SELECT count(*) FROM pages) + (SELECT count(*) FROM page_updates) + (SELECT count(*) FROM page_snapshots)").Scan(&rows))
	assert.Zero(t, rows)
}

// TestPages_RefusesAMoveUnderItsOwnDescendant answers 409 to moving a
// page under its child. Moving the child out is the control.
func TestPages_RefusesAMoveUnderItsOwnDescendant(t *testing.T) {
	s := newStack(t)
	top := s.createPage(t, "top")
	_, body := s.call(t, "ada", http.MethodPost, "/orgs/acme/pages", `{"title":"child","parent":"`+top+`"}`)
	var child apiPage
	require.NoError(t, json.Unmarshal([]byte(body), &child))

	status, body := s.call(t, "ada", http.MethodPatch, "/orgs/acme/pages/"+top, `{"parent":"`+child.ID+`"}`)
	assert.Equal(t, http.StatusConflict, status, body)
	status, body = s.call(t, "ada", http.MethodPatch, "/orgs/acme/pages/"+child.ID, `{"parent":""}`)
	assert.Equal(t, http.StatusOK, status, body)
}

// TestPages_AnswersAsThePlatformDecides holds each refusal to its
// source: no credential the platform accepts is 401, no member of the
// organization is 403, a read the platform denies hides that page
// from the tree and refuses it, and a parent that does not exist is
// 404. A member reading the tree is the control.
func TestPages_AnswersAsThePlatformDecides(t *testing.T) {
	s := newStack(t)
	hidden := s.createPage(t, "hidden")
	s.createPage(t, "open")
	require.Len(t, s.list(t, "bob"), 2)

	status, _ := s.call(t, "nobody", http.MethodGet, "/orgs/acme/pages", "")
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = s.call(t, "eve", http.MethodGet, "/orgs/acme/pages", "")
	assert.Equal(t, http.StatusForbidden, status)

	s.platform.denyCheck("bob cowork:page-read@" + hidden)
	pages := s.list(t, "bob")
	require.Len(t, pages, 1)
	assert.Equal(t, "open", pages[0].Title)
	status, _ = s.call(t, "bob", http.MethodGet, "/orgs/acme/pages/"+hidden, "")
	assert.Equal(t, http.StatusForbidden, status)
	assert.Len(t, s.list(t, "ada"), 2, "the denial is bob's alone")

	status, _ = s.call(t, "ada", http.MethodPost, "/orgs/acme/pages", `{"title":"x","parent":"nosuchpage00"}`)
	assert.Equal(t, http.StatusNotFound, status)
}

// TestPages_RefusesWhenThePlatformCannotBeReached answers 503, rather
// than deciding alone, when the platform is down. The same request with
// the platform up is the control.
func TestPages_RefusesWhenThePlatformCannotBeReached(t *testing.T) {
	p := pool(t)
	platformSrv := httptest.NewServer(&fakePlatform{deny: map[string]bool{}})
	h, err := cli.NewHandler(cli.Config{PlatformURL: platformSrv.URL}, p)
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s := &stack{url: srv.URL, pool: p}

	status, _ := s.call(t, "ada", http.MethodGet, "/orgs/acme/pages", "")
	require.Equal(t, http.StatusOK, status)
	platformSrv.Close()
	status, body := s.call(t, "ada", http.MethodGet, "/orgs/acme/pages", "")
	assert.Equal(t, http.StatusServiceUnavailable, status, body)
}
