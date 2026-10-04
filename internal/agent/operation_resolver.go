package agent

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// OperationKind is the operation a user's handoff or durable operation
// reference was minted for.
type OperationKind string

const (
	OperationKindCreate    OperationKind = "agent:create"
	OperationKindConfigure OperationKind = "agent:configure"
	OperationKindRecover   OperationKind = "agent:recover"
)

// OperationBinding is what a handoff or an operation reference was
// minted for. RequestID is the identifier the member minted it under;
// Assignee and Epoch are the assignment snapshot it holds; BodySHA256
// is the digest of the exact request it authorizes.
type OperationBinding struct {
	Ref        string
	Kind       OperationKind
	Org        grn.GRN
	Audience   grn.GRN
	Target     grn.GRN
	RequestID  string
	Assignee   grn.GRN
	Epoch      int
	BodySHA256 [32]byte
	// ExpiresAt is the presented handoff's expiry, and zero for a
	// durable reference, which has none.
	ExpiresAt time.Time
}

// OperationResolver reads a user's delegated operation authority
// fresh, for the audience it was handed to: the grant it rests on is
// current and its actor still holds the authority it was minted under.
// Another audience's is unknown. A binding that no longer holds is
// KindPermissionDenied, one unknown is KindNotFound, and authority it
// could not read is KindUnavailable.
type OperationResolver interface {
	// ResolveReference answers the durable reference a pending
	// revision's activation carries. It checks no handoff expiry: the
	// reference outlives the handoff that minted it.
	ResolveReference(ctx context.Context, audience grn.GRN, ref string) (*OperationBinding, error)
	// ResolveHandoff answers the short-lived handoff a request
	// presents.
	ResolveHandoff(ctx context.Context, audience grn.GRN, authority string) (*OperationBinding, error)
}
