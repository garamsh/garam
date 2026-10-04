package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// LeafVerifier verifies a certificate another party received over mTLS
// and forwarded unchanged: its signature chains to the authority of
// agentGRN's organization and it names agentGRN. Validity in time is
// not judged here; the execution fence judges it at the instant it
// admits (ADR-0080). A certificate that does not verify is
// KindInvalidArgument.
type LeafVerifier interface {
	VerifyLeaf(ctx context.Context, agentGRN grn.GRN, certificatePEM []byte) (Credential, error)
}
