package api_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// TestListGroups_AsksAPageLongerThanTheServiceAnswersInSeveralCalls
// lists 1001 groups, one past what the policy service answers in one
// Evaluations call, and refuses the first and the last. The first is
// asked in the first call and the last alone in the second: two calls
// of 1000 and 1, and each answer is matched to its own group, so both
// refused groups are hidden and the other 999 are listed.
func TestListGroups_AsksAPageLongerThanTheServiceAnswersInSeveralCalls(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")

	f.decisions.EXPECT().Evaluate(mock.Anything, mock.Anything).
		Return(policydecision.Decision{Allowed: true}, nil)
	slug := createOrg(t, f, owner, "acme")
	orgGRN := grn.Org(slug)

	const total = 1001
	refused := map[string]bool{}
	for i := range total {
		name := fmt.Sprintf("g%04d", i)
		g := grn.New(slug, "group", name)
		require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(g, name, orgGRN)))
		if i == 0 || i == total-1 {
			refused[g.String()] = true
		}
	}

	var mu sync.Mutex
	var sizes []int
	f.decisions.EXPECT().Evaluations(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, qs []policydecision.Question) ([]policydecision.Decision, error) {
			mu.Lock()
			sizes = append(sizes, len(qs))
			mu.Unlock()
			out := make([]policydecision.Decision, len(qs))
			for i, q := range qs {
				out[i] = policydecision.Decision{Allowed: !refused[q.Resource.String()]}
			}
			return out, nil
		})

	rec := f.do(t, http.MethodGet, "/orgs/"+slug+"/groups", "", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	listed := map[string]bool{}
	for _, g := range decodeBody[[]apigen.Group](t, rec) {
		listed[g.Grn] = true
	}

	assert.Equal(t, []int{1000, 1}, sizes, "the questions asked in each call")
	assert.Len(t, listed, total-len(refused))
	for g := range refused {
		assert.NotContains(t, listed, g, "a refused group is hidden")
	}
}
