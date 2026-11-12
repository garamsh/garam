package grn

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pending is for a service with no manifest. A service that declares
// one is read from the generated file, so listing it here as well would
// be a second place its types are written.
func TestPendingNamesNoServiceAManifestDeclares(t *testing.T) {
	require.NotEmpty(t, pending)
	for service := range pending {
		assert.NotContains(t, Services(), service, "%s declares a manifest; drop it from pending", service)
	}
}

func TestEveryDeclaredTypeIsInTheCatalogue(t *testing.T) {
	for _, service := range Services() {
		types := Types(service)
		require.NotEmpty(t, types, service)
		for name, info := range types {
			assert.True(t, catalogue[service][name], "%s/%s", service, name)
			assert.NotEmpty(t, info.Entity, "%s/%s names no entity type", service, name)
		}
	}
	// Control: a pending type is in the catalogue and in no manifest.
	assert.True(t, catalogue["codehub"]["repo"])
	assert.Empty(t, Types("codehub"))
}

func TestActionsAreQualifiedByTheirService(t *testing.T) {
	actions := Actions()
	require.NotEmpty(t, actions)
	for _, action := range actions {
		service, _, found := strings.Cut(action.Name, ":")
		require.True(t, found, "%s names no service", action.Name)
		assert.True(t, slices.Contains(Services(), service), action.Name)
		assert.NotEmpty(t, action.Principals, action.Name)
		assert.NotEmpty(t, action.Resources, action.Name)
	}
}
