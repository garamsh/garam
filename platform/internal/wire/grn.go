package wire

import (
	"github.com/garamsh/garam/platform/internal/grn"
)

// GRNsToProto renders GRNs as the strings a request carries.
func GRNsToProto(in []grn.GRN) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		out = append(out, g.String())
	}
	return out
}
