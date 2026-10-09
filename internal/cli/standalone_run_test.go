package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMessageIssuerOf_DefaultsToTheMachineOriginAndTakesGaramsSpelling
// holds the issuer the runtime compares byte for byte: the machine
// listener's origin by default, the one named otherwise, and a spelling
// garam's issuer cannot have refused rather than rendered.
func TestMessageIssuerOf_DefaultsToTheMachineOriginAndTakesGaramsSpelling(t *testing.T) {
	issuer, err := messageIssuerOf("", "https://garam.example:8443")
	require.NoError(t, err)
	assert.Equal(t, "https://garam.example:8443", string(issuer))

	issuer, err = messageIssuerOf("https://machine.garam.example", "https://10.0.0.1:8443")
	require.NoError(t, err)
	assert.Equal(t, "https://machine.garam.example", string(issuer))

	_, err = messageIssuerOf("https://GARAM.example", "https://garam.example")
	assert.Error(t, err)
	_, err = messageIssuerOf("", "https://GARAM.example")
	assert.Error(t, err, "a machine URL in another spelling is no default")
}
