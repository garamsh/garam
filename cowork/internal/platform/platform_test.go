package platform_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// reads allows the page read and nothing else.
type reads struct{ platform.Identity }

func (reads) Allowed(_ context.Context, _ platform.Caller, _ string, checks []platform.Check) ([]bool, error) {
	out := make([]bool, len(checks))
	for i, c := range checks {
		out[i] = c.Action == platform.ActionPageRead
	}
	return out, nil
}

// TestRequire_RefusesWhenAnyCheckIsDenied holds that one denied check
// refuses the whole, and all allowed passes.
func TestRequire_RefusesWhenAnyCheckIsDenied(t *testing.T) {
	p := reads{}
	caller := platform.Caller{}

	require.NoError(t, platform.Require(context.Background(), p, caller, "acme", platform.Check{Action: platform.ActionPageRead}))
	err := platform.Require(context.Background(), p, caller, "acme",
		platform.Check{Action: platform.ActionPageRead}, platform.Check{Action: platform.ActionPageWrite})
	assert.ErrorIs(t, err, platform.ErrNotAllowed)
}
