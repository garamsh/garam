package cli

import "github.com/garamsh/garam/platform/sdk/errorx"

// ExitCode is the status the binary exits with for err: 2 for a
// command used wrongly, 1 for any other failure (ADR-0122's contract,
// which the client commands join in a later milestone).
func ExitCode(err error) int {
	if errorx.IsKind(err, errorx.KindInvalidArgument) {
		return 2
	}
	return 1
}
