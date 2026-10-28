package wire_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/wire"
)

func TestGRNsToProto(t *testing.T) {
	assert.Empty(t, wire.GRNsToProto(nil))
	assert.Equal(t,
		[]string{"grn:root:default:user:alice", "grn:acme:default:group:eng"},
		wire.GRNsToProto([]grn.GRN{
			grn.New("root", "default", "user", "alice"),
			grn.New("acme", "default", "group", "eng"),
		}))
}
