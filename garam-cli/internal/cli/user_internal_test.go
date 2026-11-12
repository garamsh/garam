package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLastSegment_IsTheIdentifierOfAGRN(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"an organization":            {"grn:root/iam/org/acme", "acme"},
		"an agent":                   {"grn:acme/iam/agent/9f2ac1b40d8e7a35", "9f2ac1b40d8e7a35"},
		"a string that is not a GRN": {"acme", "acme"},
		"a name with a locator":      {"grn:acme/iam/agent/x#note", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, lastSegment(tc.in))
		})
	}
}
