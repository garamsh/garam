package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// register records an operator of acme and answers its GRN.
func register(t *testing.T, s *operator.PostgresStore, id string) grn.GRN {
	t.Helper()
	o, err := operator.New(orgRootGRN("acme"), id, scopedGRN("acme", "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.Register(context.Background(), o)
	require.NoError(t, err)
	return o.GRN()
}

// stateOf reads one operator out of the organization's listing, which
// is the read the console makes.
func stateOf(t *testing.T, s *operator.PostgresStore, g grn.GRN) *operator.Operator {
	t.Helper()
	listed, err := s.ListByOrg(context.Background(), orgRootGRN("acme"))
	require.NoError(t, err)
	for _, o := range listed {
		if o.GRN() == g {
			return o
		}
	}
	t.Fatalf("operator %s is not in the listing", g)
	return nil
}

// TestOperatorRegister_AnswersACertificateIssuedBeforeIt covers the
// registration's read-back. `garam issue-certificate` records a
// certificate for a GRN nobody registered (000019 carries no foreign
// key to `operators`), so registering that GRN afterwards answers when
// garam issued it, read in the same transaction rather than assumed.
//
// The control is the operator beside it: the same registration call,
// with no certificate already recorded, answers none. Without it, a
// Register that answered a date for everything would pass.
func TestOperatorRegister_AnswersACertificateIssuedBeforeIt(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operator.NewPostgresStore(p)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	g := scopedGRN("acme", operator.ServiceType, "minted-first")
	mintedAt := time.Now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.Adopt(ctx, operator.Current{Operator: g, Serial: "21", IssuedAt: mintedAt}))

	o, err := operator.New(orgRootGRN("acme"), "minted-first", scopedGRN("acme", "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	registered, err := s.Register(ctx, o)
	require.NoError(t, err)
	plain := register(t, s, "minted-never")

	assert.Equal(t, mintedAt, registered.CertificateIssuedAt().UTC(),
		"registering a GRN that already holds a certificate answers what garam holds")
	assert.Equal(t, mintedAt, stateOf(t, s, o.GRN()).CertificateIssuedAt().UTC())
	assert.True(t, stateOf(t, s, plain).CertificateIssuedAt().IsZero())
}

// TestOperatorCertificateIssuedAt_MovesWithEachRenewal is why the field
// is named for the certificate and not for an enrolment. ADR-0046
// keeps one row per operator holding the current certificate alone, and
// a renewal moves it, so the timestamp answers when garam last signed
// for the operator.
func TestOperatorCertificateIssuedAt_MovesWithEachRenewal(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operator.NewPostgresStore(p)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	g := register(t, s, "renewing")
	issuedAt := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.Adopt(ctx, operator.Current{Operator: g, Serial: "31", IssuedAt: issuedAt}))
	require.Equal(t, issuedAt, stateOf(t, s, g).CertificateIssuedAt().UTC())

	renewedAt := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.Advance(ctx, operator.Current{
		Operator: g, Serial: "32", IssuedAt: renewedAt,
	}, "31"))

	assert.Equal(t, renewedAt, stateOf(t, s, g).CertificateIssuedAt().UTC(),
		"the renewal moved the record this reads")
}
