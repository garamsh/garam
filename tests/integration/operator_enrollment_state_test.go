package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// register records an operator of acme and answers the token beside it,
// which is the only form the secret exists in.
func register(t *testing.T, s *operator.PostgresStore, id string) (grn.GRN, *operator.Enrollment) {
	t.Helper()
	registeredBy := scopedGRN("acme", "user", "ada")
	o, err := operator.New(orgRootGRN("acme"), id, registeredBy)
	require.NoError(t, err)
	e, err := operator.NewEnrollment(o.GRN(), registeredBy)
	require.NoError(t, err)
	_, err = s.Register(context.Background(), o, e)
	require.NoError(t, err)
	return o.GRN(), e
}

// expireToken ages the operator's token past its window. EnrollmentTTL
// is a constant and nothing injects a clock into the statement that
// reads it, so the row is moved to where ten minutes would put it.
func expireToken(t *testing.T, p *pgxpool.Pool, g grn.GRN) {
	t.Helper()
	tag, err := p.Exec(context.Background(),
		`UPDATE operator_enrollment_tokens SET expires_at = now() - interval '1 minute'
		 WHERE operator_grn = $1`, g.String())
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "there was a token to age")
}

// stateOf reads one operator's state out of the organization's listing,
// which is the read the console makes.
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

// TestOperatorEnrollmentState_TellsACertificateFromATokenFromNeither
// covers what the operator listing answers about enrolment, which is
// derived from two tables beside the record and stored in neither.
//
// The four cases are fired at one query, because the defect this guards
// is a query that answers the same for two of them. Dropping
// `expires_at > now()` from the token join makes an expired token read
// `pending` beside a live one; dropping the certificate join makes a
// certificate read `expired` beside a token that ran out. Each collapse
// is what one of the inequalities below fails on.
//
// `swept` is the control on `expired`: it reaches the same answer with
// no token row at all, so what the expired one is answered by is the
// expiry rather than the row's absence. That distinction is what the
// row count below pins, and it is not free — minting sweeps every
// expired token there is, so a registration after this one would take
// the row away and leave the two cases identical. The ageing is last
// for that reason.
func TestOperatorEnrollmentState_TellsACertificateFromATokenFromNeither(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operator.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	// Enrolled: the token was spent and a certificate recorded, which
	// is what enrolling does.
	enrolled, enrolledToken := register(t, s, "enrolled")
	_, err := s.Spend(ctx, enrolledToken.TokenHash())
	require.NoError(t, err)
	issuedAt := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.Adopt(ctx, operator.Current{
		Operator: enrolled, Serial: "11", IssuedAt: issuedAt,
	}))

	// Pending: registered, and the token registering minted is inside
	// its window.
	pending, _ := register(t, s, "pending")

	// Swept: the token is gone. A spend that answered no certificate is
	// how an operator reaches this without waiting — the attempt spends
	// the token whether or not the signing succeeded.
	swept, sweptToken := register(t, s, "swept")
	_, err = s.Spend(ctx, sweptToken.TokenHash())
	require.NoError(t, err)

	// Expired: the token is still a row and is past its window. Aged
	// after the last registration, because minting sweeps these.
	expired, _ := register(t, s, "expired")
	expireToken(t, p, expired)
	require.Equal(t, 1, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, expired.String()),
		"the expired case still holds a row, which is what separates it from the swept one")
	require.Equal(t, 0, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, swept.String()),
		"the swept case holds none")

	assert.Equal(t, operator.EnrollmentStateEnrolled, stateOf(t, s, enrolled).EnrollmentState())
	assert.Equal(t, operator.EnrollmentStatePending, stateOf(t, s, pending).EnrollmentState())
	assert.Equal(t, operator.EnrollmentStateExpired, stateOf(t, s, expired).EnrollmentState())
	assert.Equal(t, operator.EnrollmentStateExpired, stateOf(t, s, swept).EnrollmentState())

	assert.NotEqual(t, stateOf(t, s, pending).EnrollmentState(), stateOf(t, s, expired).EnrollmentState(),
		"a token past its window answers as no token, not as one somebody still holds")
	assert.NotEqual(t, stateOf(t, s, enrolled).EnrollmentState(), stateOf(t, s, expired).EnrollmentState(),
		"a certificate is not read off the token table")

	// The timestamp rides on the certificate and on nothing else, so an
	// operator with none carries the zero time rather than a date the
	// console would print.
	assert.Equal(t, issuedAt, stateOf(t, s, enrolled).CertificateIssuedAt().UTC())
	for _, g := range []grn.GRN{pending, expired, swept} {
		assert.True(t, stateOf(t, s, g).CertificateIssuedAt().IsZero(), g.String())
	}

	// Every state survives the round trip through Get, which is the
	// other read of the same join.
	got, err := s.Get(ctx, expired)
	require.NoError(t, err)
	assert.Equal(t, operator.EnrollmentStateExpired, got.EnrollmentState())
}

// TestOperatorEnrollmentState_ACertificateOutranksATokenBesideIt covers
// the one case the three states are an ordering of rather than a
// partition. `garam issue-certificate` records a certificate for a GRN
// nobody registered (000019 carries no foreign key to `operators`), and
// registering that GRN afterwards mints a token beside it — so a
// registration can hold both at once, and the registration's own answer
// is where it first shows.
func TestOperatorEnrollmentState_ACertificateOutranksATokenBesideIt(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operator.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	// The certificate is minted out of band, before anything registers
	// the operator.
	g := scopedGRN("acme", operator.ServiceType, "minted-first")
	mintedAt := time.Now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.Adopt(ctx, operator.Current{Operator: g, Serial: "21", IssuedAt: mintedAt}))

	registeredBy := scopedGRN("acme", "user", "ada")
	o, err := operator.New(orgRootGRN("acme"), "minted-first", registeredBy)
	require.NoError(t, err)
	e, err := operator.NewEnrollment(o.GRN(), registeredBy)
	require.NoError(t, err)
	registered, err := s.Register(ctx, o, e)
	require.NoError(t, err)

	// The control is the operator beside it: the same registration
	// call, with no certificate already recorded, answers `pending`.
	// Without it, a Register that read nothing at all would pass the
	// assertion above by answering `enrolled` to everything.
	plain, _ := register(t, s, "minted-never")

	assert.Equal(t, operator.EnrollmentStateEnrolled, registered.EnrollmentState(),
		"registering a GRN that already holds a certificate answers what garam holds")
	assert.Equal(t, mintedAt, registered.CertificateIssuedAt().UTC())
	assert.Equal(t, operator.EnrollmentStatePending, stateOf(t, s, plain).EnrollmentState())
	assert.Equal(t, operator.EnrollmentStateEnrolled, stateOf(t, s, o.GRN()).EnrollmentState())
}

// TestOperatorCertificateIssuedAt_MovesWithEachRenewal is why the field
// is named for the certificate and not for the enrolment. ADR-0046
// keeps one row per operator holding the current certificate alone, and
// a renewal moves it, so the timestamp answers when garam last signed
// for the operator. A field called `enrolledAt` would be wrong one
// renewal after it was written — about eight hours, at the default
// lifetime.
func TestOperatorCertificateIssuedAt_MovesWithEachRenewal(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operator.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	g, _ := register(t, s, "renewing")
	enrolledAt := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.Adopt(ctx, operator.Current{Operator: g, Serial: "31", IssuedAt: enrolledAt}))
	require.Equal(t, enrolledAt, stateOf(t, s, g).CertificateIssuedAt().UTC())

	renewedAt := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.Advance(ctx, operator.Current{
		Operator: g, Serial: "32", IssuedAt: renewedAt,
	}, "31"))

	assert.Equal(t, renewedAt, stateOf(t, s, g).CertificateIssuedAt().UTC(),
		"the renewal moved the record this reads")
	assert.NotEqual(t, enrolledAt, stateOf(t, s, g).CertificateIssuedAt().UTC())
	assert.Equal(t, operator.EnrollmentStateEnrolled, stateOf(t, s, g).EnrollmentState(),
		"a renewal leaves the state where it was")
}
