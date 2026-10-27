package collab_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/collab"
)

// TestDecode_ReadsWhatY_WebsocketWrites decodes bytes as y-websocket's
// provider writes them: a step 1 with a two-byte state vector, an
// update of 200 bytes, whose length takes a two-byte varuint, and an
// awareness update. Each re-encodes to the same bytes.
func TestDecode_ReadsWhatY_WebsocketWrites(t *testing.T) {
	long := make([]byte, 200)
	long[199] = 7
	cases := []struct {
		name string
		wire []byte
		want collab.Message
	}{
		{"step 1", []byte{0, 0, 2, 1, 0}, collab.Message{Kind: collab.MessageKindSync, Sync: collab.SyncKindStep1, Payload: []byte{1, 0}}},
		{"update", append([]byte{0, 2, 0xc8, 0x01}, long...), collab.Message{Kind: collab.MessageKindSync, Sync: collab.SyncKindUpdate, Payload: long}},
		{"awareness", []byte{1, 3, 9, 8, 7}, collab.Message{Kind: collab.MessageKindAwareness, Payload: []byte{9, 8, 7}}},
		{"query awareness", []byte{3}, collab.Message{Kind: collab.MessageKindQueryAwareness}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := collab.Decode(c.wire)
			require.NoError(t, err)
			assert.Equal(t, c.want.Kind, got.Kind)
			assert.Equal(t, c.want.Sync, got.Sync)
			assert.Equal(t, len(c.want.Payload), len(got.Payload))
			assert.Equal(t, c.wire, got.Encode())
		})
	}
}

// TestDecode_RefusesWhatDoesNotDecode refuses a length past the end,
// trailing bytes, an unknown kind and an unknown sync step. The same
// step 1 whole is the control.
func TestDecode_RefusesWhatDoesNotDecode(t *testing.T) {
	_, err := collab.Decode([]byte{0, 0, 2, 1, 0})
	require.NoError(t, err)
	for name, wire := range map[string][]byte{
		"length past the end": {0, 0, 5, 1, 0},
		"trailing bytes":      {0, 0, 2, 1, 0, 9},
		"unknown kind":        {9},
		"unknown sync step":   {0, 3, 0},
		"empty":               {},
		"varuint never ends":  {0x80, 0x80},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := collab.Decode(wire)
			assert.ErrorIs(t, err, collab.ErrMalformedMessage)
		})
	}
}
