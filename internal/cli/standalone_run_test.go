package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMessageIssuerOf_TakesOnlyTheIssuerNamedInGaramsSpelling holds the
// issuer the runtime compares byte for byte: the one named, held to
// garam's spelling, and none at all where none is named, since no
// address the host reaches garam at stands in for an identity.
func TestMessageIssuerOf_TakesOnlyTheIssuerNamedInGaramsSpelling(t *testing.T) {
	issuer, err := messageIssuerOf("https://machine.dev.garam.sh")
	require.NoError(t, err)
	assert.Equal(t, "https://machine.dev.garam.sh", string(issuer))

	issuer, err = messageIssuerOf("")
	require.NoError(t, err)
	assert.Empty(t, issuer, "nothing named is no issuer")

	_, err = messageIssuerOf("https://GARAM.example")
	assert.Error(t, err)
}
