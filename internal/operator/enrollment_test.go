package operator_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

func TestNewEnrollment_MintsAnUnguessableTokenForTheOperatorNamed(t *testing.T) {
	subject := operatorGRN("acme", "field-one")
	minter := grn.New(grn.RootOwner, grn.DefaultProject, "user", "u-1")

	first, err := operator.NewEnrollment(subject, minter)
	require.NoError(t, err)
	second, err := operator.NewEnrollment(subject, minter)
	require.NoError(t, err)

	assert.Equal(t, subject, first.Operator())
	assert.Equal(t, minter, first.MintedBy())
	assert.NotEqual(t, first.Token(), second.Token(), "each token is drawn afresh")
	assert.Equal(t, operator.EnrollmentTTL, first.ExpiresAt().Sub(first.MintedAt()))

	// What is stored is not what the person copies, so a row read out of
	// a dump enrols nothing.
	assert.NotEqual(t, first.Token(), first.TokenHash())
	assert.Equal(t, operator.HashToken(first.Token()), first.TokenHash())
}

// TestNewEnrollment_RefusesASubjectThatIsNotAnOperator holds the
// operator subject beside the refused one: what refuses an agent is the
// service type and not a constructor that mints for nobody.
func TestNewEnrollment_RefusesASubjectThatIsNotAnOperator(t *testing.T) {
	minter := grn.New(grn.RootOwner, grn.DefaultProject, "user", "u-1")

	_, err := operator.NewEnrollment(operatorGRN("acme", "field-one"), minter)
	require.NoError(t, err)

	_, err = operator.NewEnrollment(agentGRN("acme", "bot-1"), minter)
	require.ErrorIs(t, err, operator.ErrNotAnOperator)
}

// TestService_EnrollSignsTheRequestForTheOperatorTheTokenNames is the
// enrollment itself: what comes back names the token's operator, is
// signed over what the caller sent, and carries no private key because
// garam generated none.
func TestService_EnrollSignsTheRequestForTheOperatorTheTokenNames(t *testing.T) {
	store, enrollments, issuer := newRecords(), newTokens(), &signer{}
	svc, c := serviceEnrolling(t, store, enrollments, issuer)
	subject := operatorGRN("acme", "field-one")
	token := enrollments.mint("a-token", subject)

	certificate, err := svc.Enroll(context.Background(), token, operator.CertificateRequest("a request"))
	require.NoError(t, err)

	assert.Equal(t, subject, certificate.Subject)
	assert.Equal(t, operator.Serial("serial-1"), certificate.Serial)
	assert.Equal(t, []string{"a request"}, issuer.requested,
		"garam signs the key the operator generated and no other")
	assert.Equal(t, []grn.GRN{subject}, issuer.subject,
		"the certificate names the operator the token was minted for")

	// The enrollment starts the lineage a renewal is measured against,
	// as the out-of-band mint does.
	assert.Equal(t, operator.Current{Operator: subject, Serial: "serial-1", IssuedAt: c.at},
		store.current[subject])
}

// TestService_EnrollSpendsTheTokenWhetherOrNotItSigns is single use
// from both sides: the second call with an accepted token is refused,
// and so is the second call with a token whose signing failed. The
// accepted first call is what says the refusal is the token's doing.
func TestService_EnrollSpendsTheTokenWhetherOrNotItSigns(t *testing.T) {
	store, enrollments, issuer := newRecords(), newTokens(), &signer{}
	svc, _ := serviceEnrolling(t, store, enrollments, issuer)
	spent := operatorGRN("acme", "field-one")
	enrollments.mint("spent-token", spent)

	_, err := svc.Enroll(context.Background(), "spent-token", operator.CertificateRequest("a request"))
	require.NoError(t, err)

	_, err = svc.Enroll(context.Background(), "spent-token", operator.CertificateRequest("a request"))
	require.ErrorIs(t, err, operator.ErrEnrollmentInvalid)

	refused := operatorGRN("acme", "field-two")
	enrollments.mint("refused-token", refused)
	issuer.err = assert.AnError
	_, err = svc.Enroll(context.Background(), "refused-token", operator.CertificateRequest("a request"))
	require.Error(t, err)

	issuer.err = nil
	_, err = svc.Enroll(context.Background(), "refused-token", operator.CertificateRequest("a request"))
	require.ErrorIs(t, err, operator.ErrEnrollmentInvalid,
		"a token is spent by the attempt, not by the certificate")
}

// TestService_EnrollRefusesATokenNobodyMinted holds a minted token
// beside the unknown one, so what refuses is the token and not a
// service that enrols nobody.
func TestService_EnrollRefusesATokenNobodyMinted(t *testing.T) {
	store, enrollments, issuer := newRecords(), newTokens(), &signer{}
	svc, _ := serviceEnrolling(t, store, enrollments, issuer)
	enrollments.mint("a-token", operatorGRN("acme", "field-one"))

	_, err := svc.Enroll(context.Background(), "another-token", operator.CertificateRequest("a request"))
	require.ErrorIs(t, err, operator.ErrEnrollmentInvalid)
	assert.Equal(t, 0, issuer.signed, "nothing is signed for a token that buys nothing")

	_, err = svc.Enroll(context.Background(), "a-token", operator.CertificateRequest("a request"))
	require.NoError(t, err)
}
