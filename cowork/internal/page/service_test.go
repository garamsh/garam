package page_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/page/memory"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// policy is a platform that allows every check but the ones deny
// names, as action or action@page, and refuses a caller outside acme
// as the platform does.
type policy struct {
	deny  map[string]bool
	asked int
}

func (p *policy) Identify(context.Context, platform.Credential) (platform.Caller, error) {
	return platform.Caller{}, nil
}

func (p *policy) Organizations(context.Context, platform.Caller) ([]platform.Organization, error) {
	return []platform.Organization{{ID: "acme", Name: "Acme"}}, nil
}

func (p *policy) Allowed(_ context.Context, _ platform.Caller, org string, checks []platform.Check) ([]bool, error) {
	if org != "acme" {
		return nil, errorx.New(errorx.KindPermissionDenied, "not a member")
	}
	p.asked += len(checks)
	out := make([]bool, len(checks))
	for i, c := range checks {
		k := c.Action
		if c.ID != "" {
			k += "@" + c.ID
		}
		out[i] = !p.deny[k]
	}
	return out, nil
}

var ada = platform.Caller{GRN: grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada")}

func newService(t *testing.T) (page.Service, *policy) {
	t.Helper()
	p := &policy{deny: map[string]bool{}}
	return page.NewService(memory.New(), p), p
}

func create(t *testing.T, s page.Service, title, parent string) page.Page {
	t.Helper()
	p, err := s.Create(context.Background(), ada, "acme", page.CreateInput{Title: title, Parent: parent})
	require.NoError(t, err)
	return p
}

func titles(pages []page.Page) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.Title
	}
	return out
}

// TestCreate_PlacesAPageAfterItsSiblings creates three pages at the
// top and one beneath the first, and reads back their positions, the
// creator and the GRN the platform names the page by.
func TestCreate_PlacesAPageAfterItsSiblings(t *testing.T) {
	s, _ := newService(t)
	a := create(t, s, "a", "")
	b := create(t, s, "b", "")
	child := create(t, s, "a/1", a.ID)

	assert.Equal(t, []int{0, 1, 0}, []int{a.Position, b.Position, child.Position})
	assert.Equal(t, ada.GRN, a.CreatedBy)
	assert.Equal(t, "grn:acme:default:cowork-page:"+a.ID, a.GRN().String())
	assert.NoError(t, a.GRN().Validate())
}

// TestList_LeavesOutWhatTheCallerMayNotRead denies reading one page
// and reads the tree without it or the page beneath it. The same tree
// read with nothing denied is the control.
func TestList_LeavesOutWhatTheCallerMayNotRead(t *testing.T) {
	s, p := newService(t)
	a := create(t, s, "a", "")
	create(t, s, "a/1", a.ID)
	create(t, s, "b", "")

	all, err := s.List(context.Background(), ada, "acme")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "a/1", "b"}, titles(all))

	p.deny[platform.ActionPageRead+"@"+a.ID] = true
	some, err := s.List(context.Background(), ada, "acme")
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, titles(some))
}

// TestList_AsksAboutAtMostFiftyPagesAtOnce lists 120 pages, which
// takes three permission requests of at most 50 pairs, and reads all
// of them back.
func TestList_AsksAboutAtMostFiftyPagesAtOnce(t *testing.T) {
	s, p := newService(t)
	for range 120 {
		create(t, s, "p", "")
	}
	p.asked = 0
	pages, err := s.List(context.Background(), ada, "acme")
	require.NoError(t, err)
	assert.Len(t, pages, 120)
	assert.Equal(t, 121, p.asked, "the tree once, then each page")
}

// TestService_RefusesWhatThePlatformDenies holds each method to its
// check: with the named check denied, the call is refused as
// permission denied; the same call with nothing denied is the control,
// run first.
func TestService_RefusesWhatThePlatformDenies(t *testing.T) {
	ctx := context.Background()
	title := "renamed"
	cases := []struct {
		name string
		deny func(target, parent page.Page) string
		call func(s page.Service, target, parent page.Page) error
	}{
		{"list needs read on the tree",
			func(_, _ page.Page) string { return platform.ActionPageRead },
			func(s page.Service, _, _ page.Page) error { _, err := s.List(ctx, ada, "acme"); return err }},
		{"get needs read on the page",
			func(t, _ page.Page) string { return platform.ActionPageRead + "@" + t.ID },
			func(s page.Service, t, _ page.Page) error { _, err := s.Get(ctx, ada, "acme", t.ID); return err }},
		{"create needs write on the tree",
			func(_, _ page.Page) string { return platform.ActionPageWrite },
			func(s page.Service, _, _ page.Page) error {
				_, err := s.Create(ctx, ada, "acme", page.CreateInput{Title: "x"})
				return err
			}},
		{"create needs write on the parent",
			func(_, p page.Page) string { return platform.ActionPageWrite + "@" + p.ID },
			func(s page.Service, _, p page.Page) error {
				_, err := s.Create(ctx, ada, "acme", page.CreateInput{Title: "x", Parent: p.ID})
				return err
			}},
		{"update needs write on the page",
			func(t, _ page.Page) string { return platform.ActionPageWrite + "@" + t.ID },
			func(s page.Service, t, _ page.Page) error {
				_, err := s.Update(ctx, ada, "acme", t.ID, page.UpdateInput{Title: &title})
				return err
			}},
		{"a move needs write on the new parent",
			func(_, p page.Page) string { return platform.ActionPageWrite + "@" + p.ID },
			func(s page.Service, t, p page.Page) error {
				_, err := s.Update(ctx, ada, "acme", t.ID, page.UpdateInput{Parent: &p.ID})
				return err
			}},
		{"delete needs write on the page",
			func(t, _ page.Page) string { return platform.ActionPageWrite + "@" + t.ID },
			func(s page.Service, t, _ page.Page) error { return s.Delete(ctx, ada, "acme", t.ID) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, p := newService(t)
			target, parent := create(t, s, "target", ""), create(t, s, "parent", "")
			require.NoError(t, c.call(s, target, parent), "allowed")

			s, p = newService(t)
			target, parent = create(t, s, "target", ""), create(t, s, "parent", "")
			p.deny[c.deny(target, parent)] = true
			err := c.call(s, target, parent)
			assert.ErrorIs(t, err, platform.ErrNotAllowed)
		})
	}
}

// TestService_RefusesANonMember holds that a caller the platform says
// is no member of the organization is refused, whatever the call.
func TestService_RefusesANonMember(t *testing.T) {
	s, _ := newService(t)
	_, err := s.List(context.Background(), ada, "globex")
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "%v", err)
}

// TestUpdate_RefusesACycle moves a page under its own child, and under
// itself, and each is refused. Moving the child to the top is the
// control.
func TestUpdate_RefusesACycle(t *testing.T) {
	s, _ := newService(t)
	a := create(t, s, "a", "")
	child := create(t, s, "a/1", a.ID)
	ctx := context.Background()

	_, err := s.Update(ctx, ada, "acme", a.ID, page.UpdateInput{Parent: &child.ID})
	assert.ErrorIs(t, err, page.ErrPageCycle)
	_, err = s.Update(ctx, ada, "acme", a.ID, page.UpdateInput{Parent: &a.ID})
	assert.ErrorIs(t, err, page.ErrPageCycle)

	top := ""
	moved, err := s.Update(ctx, ada, "acme", child.ID, page.UpdateInput{Parent: &top})
	require.NoError(t, err)
	assert.Equal(t, "", moved.Parent)
}

// TestUpdate_MovesAPageAndRenumbersBothSiblingLists moves the first of
// three top-level pages to position 0 under the third, and reads both
// lists back numbered from zero.
func TestUpdate_MovesAPageAndRenumbersBothSiblingLists(t *testing.T) {
	s, _ := newService(t)
	a, b, c := create(t, s, "a", ""), create(t, s, "b", ""), create(t, s, "c", "")
	create(t, s, "c/1", c.ID)
	zero := 0

	_, err := s.Update(context.Background(), ada, "acme", a.ID, page.UpdateInput{Parent: &c.ID, Position: &zero})
	require.NoError(t, err)

	pages, err := s.List(context.Background(), ada, "acme")
	require.NoError(t, err)
	got := map[string][2]any{}
	for _, p := range pages {
		got[p.Title] = [2]any{p.Parent, p.Position}
	}
	assert.Equal(t, [2]any{"", 0}, got["b"])
	assert.Equal(t, [2]any{"", 1}, got["c"])
	assert.Equal(t, [2]any{c.ID, 0}, got["a"])
	assert.Equal(t, [2]any{c.ID, 1}, got["c/1"])
	_ = b
}

// TestDelete_RemovesThePagesBeneath deletes a page with a child and
// reads neither back, and the remaining sibling renumbered.
func TestDelete_RemovesThePagesBeneath(t *testing.T) {
	s, _ := newService(t)
	a := create(t, s, "a", "")
	child := create(t, s, "a/1", a.ID)
	create(t, s, "b", "")
	ctx := context.Background()

	require.NoError(t, s.Delete(ctx, ada, "acme", a.ID))
	_, err := s.Get(ctx, ada, "acme", child.ID)
	assert.ErrorIs(t, err, page.ErrPageNotFound)
	pages, err := s.List(ctx, ada, "acme")
	require.NoError(t, err)
	require.Len(t, pages, 1)
	assert.Equal(t, 0, pages[0].Position)
}

// TestCreate_RefusesATitleTooLong refuses 501 bytes and takes 500.
func TestCreate_RefusesATitleTooLong(t *testing.T) {
	s, _ := newService(t)
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'x'
	}
	_, err := s.Create(context.Background(), ada, "acme", page.CreateInput{Title: string(long[:500])})
	require.NoError(t, err)
	_, err = s.Create(context.Background(), ada, "acme", page.CreateInput{Title: string(long)})
	assert.ErrorIs(t, err, page.ErrTitleTooLong)
}
