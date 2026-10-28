package cli

import "github.com/garamsh/garam/lib/errorx"

// The codes the binary exits with.
const (
	ExitFailure = 1
	ExitUsage   = 2
)

// ExitCode is the status the binary exits with for err: 2 for a
// command used wrongly and 1 for any other failure.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errorx.IsKind(err, errorx.KindInvalidArgument):
		return ExitUsage
	}
	return ExitFailure
}
