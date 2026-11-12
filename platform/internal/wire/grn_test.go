package wire_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/wire"
)

func TestGRNsToProto(t *testing.T) {
	assert.Empty(t, wire.GRNsToProto(nil))
	assert.Equal(t,
		[]string{"grn:root/iam/user/alice", "grn:acme/iam/group/eng"},
		wire.GRNsToProto([]grn.GRN{
			grn.New("root", "user", "alice"),
			grn.New("acme", "group", "eng"),
		}))
}
