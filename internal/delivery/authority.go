package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// TokenExecutionContract is the adapter-facing activation and
// runtime-status contract garam owns (ADR-0084): the control token in
// Authorization and the placement token in a Garam-Placement header of
// its own (ADR-0100). Every request and answer of the lifecycle
// authority's two routes names the contract they are made under.
const TokenExecutionContract = "agent-execution.v2"

// Authority is the lifecycle authority's adapter-facing side: the
// Operator control service when managed, the local supervisor when
// standalone. It activates the generation the runtime reports and
// takes that runtime's status.
//
// An error of KindUnavailable is undecided and is retried unchanged;
// every other error is a definite refusal (ADR-0084).
type Authority interface {
	// Contract answers the contract the authority is called under,
	// which is the request identifier's first field.
	Contract() string
	// Activate asks for the binding's activation under requestID and
	// answers it with its binding verified against the one asked for.
	Activate(ctx context.Context, requestID string, b Binding, placementToken string) (*Activation, error)
	// Report carries the runtime's status as observed, under the
	// activation it was observed in.
	Report(ctx context.Context, activation string, s *RuntimeStatus, observedAt time.Time) error
}

// RequestID is the activation request identifier ADR-0084 derives: a
// version 8 UUID over SHA-256 of the contract, the binding and the
// placement token's own SHA-256, each field prefixed by its length. The
// same intent has one identifier across restarts and token refresh, and
// the token cannot be read back from it. A change of contract is new
// intent (ADR-0100).
func RequestID(contract string, b Binding, placementToken string) string {
	fingerprint := sha256.Sum256([]byte(placementToken))
	h := sha256.New()
	for _, field := range []string{
		contract, b.Agent.String(), b.Epoch, b.Generation, b.Revision,
		hex.EncodeToString(fingerprint[:]),
	} {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		h.Write(size[:])
		h.Write([]byte(field))
	}
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = u[6]&0x0f | 0x80
	u[8] = u[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
