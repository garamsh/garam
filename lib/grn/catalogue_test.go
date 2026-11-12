package grn

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEveryDeclaredTypeIsInTheCatalogue(t *testing.T) {
	for _, service := range Services() {
		types := Types(service)
		require.NotEmpty(t, types, service)
		for name, info := range types {
			assert.True(t, catalogue[service][name], "%s/%s", service, name)
			assert.NotEmpty(t, info.Entity, "%s/%s names no entity type", service, name)
		}
	}
	// Control: a service no manifest declares owns no type.
	assert.Empty(t, catalogue["billing"])
	assert.Empty(t, Types("billing"))
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
