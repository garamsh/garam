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

// remint mints the operator another token through the store and answers
// it, so a test holds the secret the caller would have been answered.
func remint(t *testing.T, s *operator.PostgresStore, g grn.GRN) (*operator.Enrollment, error) {
	t.Helper()
	e, err := operator.NewEnrollment(g, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	return e, s.Remint(context.Background(), e)
}

// TestOperatorRemint_ReplacesALiveTokenAndRefusesAnEnrolledOperator
// covers the re-mint against the tables it is decided by (ADR-0067).
//
// The three cases are the decision: an operator with no certificate is
// minted for whether or not a token is already live, and one holding a
// certificate is refused. The two accepted cases are the control on the
// refusal — they run against the same store on the same call, so what
// refuses `enrolled` is the certificate rather than the method refusing
// everything; and the refusal is the control on them, since a method
// that minted unconditionally would pass both accepted cases alone.
//
// `lapsed` is the case the statement's shape is chosen for. Its token
// row is still there and past its window, so the sweep inside the mint
// and the row the upsert lands on would be the same row if the sweep
// did not exclude the operator being minted for — an overlap whose
// outcome Postgres does not define, because a data-modifying CTE and
// the statement around it do not see each other's effects.
func TestOperatorRemint_ReplacesALiveTokenAndRefusesAnEnrolledOperator(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operator.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	// No certificate, no token: the registration's token was spent on an
	// attempt that signed nothing, which is how an operator reaches this
	// without waiting out the window.
	swept, sweptToken := register(t, s, "swept")
	_, err := s.Spend(ctx, sweptToken.TokenHash())
	require.NoError(t, err)
	require.Equal(t, operator.EnrollmentStateExpired, stateOf(t, s, swept).EnrollmentState())

	// No certificate, a live token nobody has spent.
	live, liveToken := register(t, s, "live")
	require.Equal(t, operator.EnrollmentStatePending, stateOf(t, s, live).EnrollmentState())

	// A certificate, which is the outcome the token exists to produce.
	enrolled, enrolledToken := register(t, s, "enrolled")
	_, err = s.Spend(ctx, enrolledToken.TokenHash())
	require.NoError(t, err)
	require.NoError(t, s.Adopt(ctx, operator.Current{
		Operator: enrolled, Serial: "41", IssuedAt: time.Now().UTC().Add(-time.Hour),
	}))

	// No certificate, a token row past its window and not yet swept.
	lapsed, lapsedToken := register(t, s, "lapsed")
	expireToken(t, p, lapsed)
	require.Equal(t, operator.EnrollmentStateExpired, stateOf(t, s, lapsed).EnrollmentState())

	// An operator with neither is minted for, and the token spends.
	sweptAgain, err := remint(t, s, swept)
	require.NoError(t, err)
	assert.Equal(t, operator.EnrollmentStatePending, stateOf(t, s, swept).EnrollmentState(),
		"a re-mint moves the operator back to pending")

	// An operator holding a live token is minted for too, and what it
	// held stops being spendable: two live tokens would be two
	// credential paths for one identity.
	replacement, err := remint(t, s, live)
	require.NoError(t, err)
	assert.NotEqual(t, liveToken.Token(), replacement.Token())
	_, err = s.Spend(ctx, liveToken.TokenHash())
	assert.ErrorIs(t, err, operator.ErrEnrollmentInvalid,
		"the token the re-mint replaced enrols nothing")
	spent, err := s.Spend(ctx, replacement.TokenHash())
	require.NoError(t, err, "the token that replaced it does")
	assert.Equal(t, live, spent)

	// The lapsed row is replaced rather than swept out from under the
	// insert, and the token that replaced it spends.
	lapsedReplacement, err := remint(t, s, lapsed)
	require.NoError(t, err)
	assert.NotEqual(t, lapsedToken.Token(), lapsedReplacement.Token())
	assert.Equal(t, operator.EnrollmentStatePending, stateOf(t, s, lapsed).EnrollmentState())
	spent, err = s.Spend(ctx, lapsedReplacement.TokenHash())
	require.NoError(t, err)
	assert.Equal(t, lapsed, spent)

	// An operator that already enrolled is refused, and the operators
	// beside it were minted for on the same store: the certificate is
	// what refuses, not the method.
	_, err = remint(t, s, enrolled)
	assert.ErrorIs(t, err, operator.ErrOperatorAlreadyEnrolled)
	assert.Equal(t, operator.EnrollmentStateEnrolled, stateOf(t, s, enrolled).EnrollmentState())
	assert.Equal(t, 0, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, enrolled.String()),
		"the refused mint wrote no row")

	// An operator the organization never registered is a miss.
	_, err = remint(t, s, scopedGRN("acme", operator.ServiceType, "never-registered"))
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)

	// One token per operator, which is the schema's (000024) and not
	// this method's to remember. `swept` still holds the one it was
	// minted above, so the count is over a table that has rows.
	assert.Equal(t, 1, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, swept.String()))
	assert.Equal(t, 0, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens
		    WHERE operator_grn IS NOT NULL
		      AND operator_grn IN (
		            SELECT operator_grn FROM operator_enrollment_tokens
		            GROUP BY operator_grn HAVING count(*) > 1)`),
		"no operator holds two")
	_, err = s.Spend(ctx, sweptAgain.TokenHash())
	assert.NoError(t, err, "the token minted for the swept operator spends")
}

// TestOperatorRemint_SweepsEveryOtherOperatorsExpiredToken is the
// control on the predicate that keeps the sweep off the row the mint
// upserts. Excluding the operator being minted for must not stop the
// sweep reaching the rest, which is what ADR-0064 decision 7 has
// minting do.
func TestOperatorRemint_SweepsEveryOtherOperatorsExpiredToken(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operator.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	// Every registration is made before any token is aged, because
	// minting sweeps: ageing between two registrations would leave the
	// second one having taken the row this test is about to count.
	minting, _ := register(t, s, "minting")
	elsewhere, _ := register(t, s, "elsewhere")
	untouched, untouchedToken := register(t, s, "untouched")
	expireToken(t, p, minting)
	expireToken(t, p, elsewhere)

	require.Equal(t, 3, countRows(t, p, `SELECT count(*) FROM operator_enrollment_tokens`),
		"three rows before the mint, so a sweep has something to take")

	_, err := remint(t, s, minting)
	require.NoError(t, err)

	assert.Equal(t, 0, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, elsewhere.String()),
		"another operator's expired token is swept by the mint")
	assert.Equal(t, 1, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, minting.String()),
		"the minting operator's own row is replaced rather than swept and re-inserted")
	assert.Equal(t, 1, countRows(t,
		p, `SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, untouched.String()),
		"a live token is not swept, which is what separates the sweep from a truncate")
	_, err = s.Spend(ctx, untouchedToken.TokenHash())
	assert.NoError(t, err)
}
