package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/errorx"
)

func runIssueCertificate(t *testing.T, subjectGRN string) error {
	t.Helper()
	// The refusal under test precedes every reach, so the environment
	// is emptied to prove the refusal rather than a missing database
	// stopped the run; the control then fails there instead, which is
	// what shows it passed the check.
	t.Setenv("GARAM_DATABASE_URL", "")
	t.Setenv("GARAM_CA_KEY_FILE", "")
	root := cli.NewRootCommand()
	root.SetArgs([]string{"issue-certificate", subjectGRN, "--out-dir", t.TempDir()})
	return root.Execute()
}

// TestIssueCertificate_RefusesANonOperatorSubject: the command mints an
// operator's first certificate only, because a general one would be the
// issuing credential ADR-0015 refuses to create. The operator beside
// the refused agent is the control: it passes the check and fails
// later, on the database the emptied environment denies it, so what
// refused the agent is the check rather than anything the run could
// not reach.
func TestIssueCertificate_RefusesANonOperatorSubject(t *testing.T) {
	err := runIssueCertificate(t, "grn:acme:default:agent:bot-1")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "want an invalid-argument kind, got %v", err)
	assert.Contains(t, err.Error(), "is not an operator")

	err = runIssueCertificate(t, "grn:acme:default:operator:one")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GARAM_DATABASE_URL",
		"an operator passes the check and fails at the first reach beyond it")
}
