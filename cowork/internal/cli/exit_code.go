package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/platform/sdk/errorx"
)

// The codes a client command exits with (ADR-0122).
const (
	ExitFailure          = 1
	ExitUsage            = 2
	ExitUnauthenticated  = 3
	ExitPermissionDenied = 4
	ExitNotFound         = 5
	ExitConflict         = 6
	ExitInvalid          = 7
	ExitUnavailable      = 8
)

// ExitCode is the status the binary exits with for err, run by cmd. A
// server command exits with 2 for a command used wrongly and 1 for any
// other failure. A client command exits with ADR-0122's code for its
// kind, and with 2 for an error that has none, which is cobra's: a
// command used wrongly.
func ExitCode(cmd *cobra.Command, err error) int {
	if err == nil {
		return 0
	}
	if cmd == nil || cmd.Annotations[clientAnnotation] == "" {
		if errorx.IsKind(err, errorx.KindInvalidArgument) {
			return ExitUsage
		}
		return ExitFailure
	}
	var kinded errorx.Error
	if !errors.As(err, &kinded) {
		return ExitUsage
	}
	switch errorx.KindOf(err) {
	case errorx.KindUnauthenticated:
		return ExitUnauthenticated
	case errorx.KindPermissionDenied:
		return ExitPermissionDenied
	case errorx.KindNotFound:
		return ExitNotFound
	case errorx.KindAlreadyExists, errorx.KindFailedPrecondition, errorx.KindAborted:
		return ExitConflict
	case errorx.KindInvalidArgument, errorx.KindOutOfRange:
		return ExitInvalid
	case errorx.KindUnavailable, errorx.KindDeadlineExceeded:
		return ExitUnavailable
	}
	return ExitFailure
}
