package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/lib/errorx"
)

// userCommandAnnotation marks a command a person or a program runs
// against the browser API, whose failure ExitCode reports by kind.
const userCommandAnnotation = "garam.sh/user-command"

// Exit codes a user command ends with (ADR-0122). A server command
// ends with 1 on any failure, as before.
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

// markUserCommands annotates cmd and everything under it as a user
// command.
func markUserCommands(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[userCommandAnnotation] = "true"
	for _, c := range cmd.Commands() {
		markUserCommands(c)
	}
}

// ExitCode is the process exit code for err, returned by the command
// cmd that ran. A user command's failure is reported by its kind, so a
// program branches on the code rather than on the message; a failure
// carrying no kind there is a usage error cobra raised before the
// command ran.
func ExitCode(cmd *cobra.Command, err error) int {
	if err == nil {
		return 0
	}
	if cmd == nil || cmd.Annotations[userCommandAnnotation] == "" {
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
