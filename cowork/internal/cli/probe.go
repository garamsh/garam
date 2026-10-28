package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/lib/errorx"
)

// probeTimeout bounds one probe answer.
const probeTimeout = 5 * time.Second

// newProbeCommand constructs `cowork probe`: GET a probe URL and fail
// unless it answers 2xx. The image carries no shell, so a healthcheck
// asks the binary it supervises.
func newProbeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "probe <url>",
		Short: "Fail unless a probe URL answers ok",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, args[0], nil)
			if err != nil {
				return errorx.Wrap(err, "probe", errorx.KindInvalidArgument)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return errorx.Wrap(err, "probe", errorx.KindUnavailable)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
				return errorx.New(errorx.KindUnavailable, fmt.Sprintf("probe: %s answered %s", args[0], resp.Status))
			}
			return nil
		},
	}
}
