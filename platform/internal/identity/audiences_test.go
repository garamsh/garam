package identity_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/identity"
)

// unmanifested is the one audience no manifest names: policy serves the
// manifests rather than declaring one (ADR-0143 item 5, ADR-0149).
var unmanifested = []string{identity.AudiencePolicy}

// An audience is a service a token is minted for, so it is a service a
// manifest declares or the one named above. `make -C policy
// check-gen` runs this test and fails when it did not run.
func TestAudiencesNameAManifestServiceOrAnException(t *testing.T) {
	services := grn.Services()
	assert.NotEmpty(t, services, "the generated catalogue names no service")
	for _, audience := range identity.Audiences {
		known := slices.Contains(services, audience) || slices.Contains(unmanifested, audience)
		assert.True(t, known, "audience %q names no manifest service and is not policy", audience)
	}
}
