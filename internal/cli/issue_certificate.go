package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// newIssueCertificateCommand constructs `garam issue-certificate
// <subject-grn>`: the one bootstrap ADR-0015 leaves out of band,
// minting an operator's own first certificate. It is a command of its
// own rather than anything a serve path reaches, so no serving process
// can mint — a server answers what it is asked, and nothing it is
// asked runs this.
func newIssueCertificateCommand() *cobra.Command {
	var outDir string
	cmd := &cobra.Command{
		Use:   "issue-certificate <subject-grn>",
		Short: "Mint an operator's first certificate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return issueCertificate(cmd, args[0], outDir)
		},
	}
	cmd.Flags().StringVar(&outDir, "out-dir", "",
		"directory to write certificate.pem, key.pem and issuer.pem into (required)")
	_ = cmd.MarkFlagRequired("out-dir")
	return cmd
}

func issueCertificate(cmd *cobra.Command, subjectRaw, outDir string) error {
	ctx := cmd.Context()

	subject, err := grn.Parse(subjectRaw)
	if err != nil {
		return errorx.Wrap(err, "subject", errorx.KindInvalidArgument)
	}
	// Operator subjects only: a command that minted any identity would
	// be the issuing credential ADR-0015 refuses to create. An agent's
	// certificate already has its path — the machine surface an
	// assigned operator calls. The check precedes every reach, so a
	// refused subject touches neither the database nor the key.
	if subject.ServiceType != agent.ServiceTypeOperator {
		return errorx.New(errorx.KindInvalidArgument,
			"subject "+subject.String()+" is not an operator: this command mints an operator's first certificate only (ADR-0015)")
	}

	databaseURL := envOr("GARAM_DATABASE_URL", "")
	if databaseURL == "" {
		return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	// The command holds the key-encryption key under the custody
	// serve machine starts under (ADR-0016): a file its owner alone can
	// read, and the sealer refuses anything else.
	sealer, err := ca.NewSealerFromKeyFile(envOr("GARAM_CA_KEY_FILE", ""))
	if err != nil {
		return errorx.Wrap(err, "GARAM_CA_KEY_FILE")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	defer pool.Close()

	// The destination is prepared before anything is minted, so a
	// destination that cannot be written fails before a certificate
	// exists rather than after.
	if err := os.MkdirAll(outDir, fs.FileMode(0o700)); err != nil {
		return errorx.Wrap(err, "out-dir", errorx.KindInvalidArgument)
	}

	lifetime, err := certificateLifetime()
	if err != nil {
		return err
	}
	issuer, err := ca.NewService(ca.NewPostgresStore(pool), sealer, ca.Config{CertificateLifetime: lifetime})
	if err != nil {
		return err
	}
	// Minted through the operator domain, which records the certificate
	// as the operator's current one: what a renewal presents has to be
	// that certificate, so a re-run here is also the recovery from a
	// lineage another holder took (ADR-0046). No server root travels
	// with it — the command writes the three files below and the root
	// the deployment already holds arrives beside them.
	operators := operator.NewService(operator.NewPostgresStore(pool),
		operatorCertificates{issuer: issuer}, operator.Config{})
	issued, err := operators.Issue(ctx, subject)
	if err != nil {
		return err
	}

	// The private key exists in this answer alone, so it is written
	// where its owner alone can read it — the way key material arrives
	// (ADR-0015). The certificates are public.
	writes := []struct {
		name string
		data []byte
		perm fs.FileMode
	}{
		{"certificate.pem", issued.CertificatePEM, 0o644},
		{"key.pem", issued.PrivateKeyPEM, 0o600},
		{"issuer.pem", issued.IssuerPEM, 0o644},
	}
	for _, w := range writes {
		if err := os.WriteFile(filepath.Join(outDir, w.name), w.data, w.perm); err != nil {
			return errorx.Wrap(err, "out-dir", errorx.KindInvalidArgument)
		}
	}

	cmd.Printf("issued %s, valid until %s; written to %s\n",
		subject.String(), issued.NotAfter.UTC().Format(time.RFC3339), outDir)
	return nil
}
