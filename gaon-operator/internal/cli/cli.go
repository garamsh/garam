// Package cli is the operator's entry point. Run reads the settings and
// then refuses to start: garam's machine client and the control
// service's feed client do not exist yet, and an operator wired to
// neither would only look as though it ran.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/garamsh/garam/gaon-operator/internal/config"
	"github.com/garamsh/garam/lib/errorx"
)

// ErrNotWired is the refusal Run returns once the settings are valid.
var ErrNotWired = errorx.New(errorx.KindFailedPrecondition,
	"gaon-operator cannot start: not wired: garam machine client "+
		"(exchange, mintAgentToken, reportProvisioningState) and "+
		"control service feed client (GET /v1/operators/self/desired, "+
		"POST /v1/operators/self/agents/{agent}/status); the reconciler "+
		"in internal/agent is tested against stand-ins for both")

// Run loads the settings and returns ErrNotWired. It writes the reason
// to stderr so a deployment's log names what is missing.
func Run(_ context.Context, getenv func(string) string, stderr io.Writer) error {
	if _, err := config.Load(getenv); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}
	_, _ = fmt.Fprintln(stderr, ErrNotWired)
	return ErrNotWired
}
