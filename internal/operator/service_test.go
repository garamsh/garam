package operator_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// lifetime is what the certificates below are signed for, and the
// renewal window is a third of it. The numbers here are the test's own:
// what garam signs is bounded by internal/ca and configured by the
// deployment, and this rule reads the window off the certificate it is
// given.
const lifetime = 24 * time.Hour

func operatorGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, operator.ServiceType, id)
}

func agentGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "agent", id)
}

// records is the CertificateStore in memory. Advance makes the
// comparison the statement makes — the record moves only where it still
// holds the serial presented — because that is what the rule under test
// rests on.
type records struct {
	current map[grn.GRN]operator.Current
	err     error
}

func newRecords() *records { return &records{current: map[grn.GRN]operator.Current{}} }

func (r *records) Adopt(_ context.Context, current operator.Current) error {
	if r.err != nil {
		return r.err
	}
	r.current[current.Operator] = current
	return nil
}

func (r *records) Advance(_ context.Context, current operator.Current, predecessor operator.Serial) error {
	if r.err != nil {
		return r.err
	}
	held, recorded := r.current[current.Operator]
	if recorded && held.Serial != predecessor {
		return operator.ErrSuperseded
	}
	r.current[current.Operator] = current
	return nil
}

// signer is the Issuer in memory: one certificate per call, under a
// serial of its own, so a test can tell the successor from what it
// replaced.
type signer struct {
	signed    int
	err       error
	subject   []grn.GRN
	requested []string
}

func (s *signer) Issue(_ context.Context, subject grn.GRN) (*operator.IssuedCertificate, error) {
	certificate, err := s.sign(subject)
	if err != nil {
		return nil, err
	}
	return &operator.IssuedCertificate{
		Certificate:   *certificate,
		PrivateKeyPEM: []byte("private key"),
	}, nil
}

// Sign records the request beside the subject, so a test can assert
// that what garam signed over is what the caller sent.
func (s *signer) Sign(_ context.Context, subject grn.GRN, request operator.CertificateRequest) (*operator.Certificate, error) {
	s.requested = append(s.requested, string(request))
	return s.sign(subject)
}

func (s *signer) sign(subject grn.GRN) (*operator.Certificate, error) {
	s.subject = append(s.subject, subject)
	if s.err != nil {
		return nil, s.err
	}
	s.signed++
	return &operator.Certificate{
		Subject:        subject,
		Serial:         operator.Serial("serial-" + strconv.Itoa(s.signed)),
		CertificatePEM: []byte("certificate"),
		IssuerPEM:      []byte("issuer"),
		ServerRootPEM:  []byte("server root"),
	}, nil
}

// held is the certificate a caller is presenting: signed at issuedAt
// for lifetime, under serial.
func held(subject grn.GRN, serial operator.Serial, issuedAt time.Time) operator.Credential {
	return operator.Credential{
		Subject:   subject,
		Serial:    serial,
		NotBefore: issuedAt,
		NotAfter:  issuedAt.Add(lifetime),
	}
}

// clock is a time a test moves by hand, so a certificate ages without
// the test waiting for it.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

// tokens is the EnrollmentStore in memory. Spend removes the row, as
// the statement does, so single use is what the store enforces and not
// what the test remembers.
type tokens struct {
	live map[string]grn.GRN
}

func newTokens() *tokens { return &tokens{live: map[string]grn.GRN{}} }

// mint records a token for subject and returns the secret, as
// registering an operator would.
func (t *tokens) mint(secret string, subject grn.GRN) string {
	t.live[operator.HashToken(secret)] = subject
	return secret
}

func (t *tokens) Spend(_ context.Context, tokenHash string) (grn.GRN, error) {
	subject, live := t.live[tokenHash]
	if !live {
		return grn.GRN{}, operator.ErrEnrollmentInvalid
	}
	delete(t.live, tokenHash)
	return subject, nil
}

// service returns the service under test over its three seams and a
// clock, and the clock a test ages a certificate with.
func service(t *testing.T, store operator.CertificateStore, issuer operator.Issuer) (operator.Service, *clock) {
	t.Helper()
	return serviceEnrolling(t, store, newTokens(), issuer)
}

func serviceEnrolling(t *testing.T, store operator.CertificateStore, enrollments operator.EnrollmentStore, issuer operator.Issuer) (operator.Service, *clock) {
	t.Helper()
	c := &clock{at: time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)}
	return operator.NewService(store, enrollments, issuer, operator.Config{Now: c.now}), c
}

// TestService_IssueAdoptsTheCertificateItMinted is the bootstrap
// ADR-0032 ships, seen from the record it leaves: what the command
// mints is the certificate the renewal rule then measures against, so a
// re-run takes the lineage from whatever the operator held before.
func TestService_IssueAdoptsTheCertificateItMinted(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, c := service(t, store, issuer)
	subject := operatorGRN("acme", "one")

	first, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)
	assert.Equal(t, operator.Serial("serial-1"), first.Serial)
	assert.Equal(t, operator.Current{Operator: subject, Serial: "serial-1", IssuedAt: c.at},
		store.current[subject])

	// A second mint takes the lineage: the first certificate is now the
	// one a renewal is refused for.
	second, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)
	assert.Equal(t, operator.Serial("serial-2"), second.Serial)
	assert.Equal(t, operator.Serial("serial-2"), store.current[subject].Serial)
}

// TestService_IssueRefusesASubjectThatIsNotAnOperator holds the operator
// subject beside the refused one: what refuses an agent here is the
// service type and not a mint that answers nobody.
func TestService_IssueRefusesASubjectThatIsNotAnOperator(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, _ := service(t, store, issuer)

	_, err := svc.Issue(context.Background(), operatorGRN("acme", "one"))
	require.NoError(t, err)

	_, err = svc.Issue(context.Background(), agentGRN("acme", "bot-1"))
	require.ErrorIs(t, err, operator.ErrNotAnOperator)
	assert.Equal(t, 1, issuer.signed, "a refused subject is refused before anything is signed")
}

// TestService_RenewIssuesTheSuccessorInsideTheWindow is the renewal
// itself: a certificate two thirds through its life is replaced, and
// the record moves to what replaced it.
func TestService_RenewIssuesTheSuccessorInsideTheWindow(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, c := service(t, store, issuer)
	subject := operatorGRN("acme", "one")

	issuedAt := c.at
	first, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)

	c.at = issuedAt.Add(lifetime - lifetime/3)
	second, err := svc.Renew(context.Background(), held(subject, first.Serial, issuedAt))
	require.NoError(t, err)
	assert.Equal(t, operator.Serial("serial-2"), second.Serial)
	assert.Equal(t, []byte("private key"), second.PrivateKeyPEM, "the successor carries its own key")
	assert.Equal(t, operator.Serial("serial-2"), store.current[subject].Serial)
	assert.Equal(t, []grn.GRN{subject, subject}, issuer.subject,
		"the successor names the identity that asked for it and no other")
}

// TestService_RenewRefusesACertificateWithTooMuchLifeLeft holds the
// window's two ends against each other: one moment before it opens the
// same certificate is refused, and one moment after it is renewed. A
// refusal alone would hold for a rule that refuses every renewal.
func TestService_RenewRefusesACertificateWithTooMuchLifeLeft(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, c := service(t, store, issuer)
	subject := operatorGRN("acme", "one")

	issuedAt := c.at
	first, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)
	credential := held(subject, first.Serial, issuedAt)

	c.at = issuedAt.Add(lifetime - lifetime/3 - time.Second)
	_, err = svc.Renew(context.Background(), credential)
	require.ErrorIs(t, err, operator.ErrRenewalTooEarly)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition),
		"the caller is told to ask again rather than to give up")
	assert.Equal(t, 1, issuer.signed, "nothing is signed for a renewal that is refused")

	c.at = c.at.Add(time.Second)
	_, err = svc.Renew(context.Background(), credential)
	require.NoError(t, err)
}

// TestService_RenewRefusesAnExpiredCertificate is the bootstrap
// boundary: a certificate with nothing left renews nothing, and the
// next one is minted out of band. The listener refuses such a
// certificate a handshake earlier (internal/machine), so this is the
// same boundary stated where the rule is.
//
// The control is the same certificate one moment before it runs out,
// which renews — so what refuses the second call is the expiry rather
// than the window it is also at the end of.
func TestService_RenewRefusesAnExpiredCertificate(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, c := service(t, store, issuer)
	subject := operatorGRN("acme", "one")

	issuedAt := c.at
	first, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)
	credential := held(subject, first.Serial, issuedAt)

	c.at = issuedAt.Add(lifetime - time.Second)
	second, err := svc.Renew(context.Background(), credential)
	require.NoError(t, err)

	c.at = issuedAt.Add(lifetime)
	_, err = svc.Renew(context.Background(), held(subject, second.Serial, issuedAt))
	require.ErrorIs(t, err, operator.ErrCertificateExpired)
	assert.Equal(t, 2, issuer.signed, "nothing is signed for a certificate that has run out")
}

// TestService_RenewRefusesACertificateAlreadyReplaced is what bounds a
// stolen certificate while the organization still wants the operator
// it was taken from: two holders of one identity, and only the one
// holding the certificate garam last issued carries the lineage on.
// The other still authenticates — nothing here takes that away — and
// renews nothing, so it expires.
func TestService_RenewRefusesACertificateAlreadyReplaced(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, c := service(t, store, issuer)
	subject := operatorGRN("acme", "one")

	issuedAt := c.at
	first, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)
	stolen := held(subject, first.Serial, issuedAt)

	c.at = issuedAt.Add(lifetime - lifetime/3)
	second, err := svc.Renew(context.Background(), stolen)
	require.NoError(t, err)

	// The thief presents the copy it took, inside the same window.
	certificate, err := svc.Renew(context.Background(), stolen)
	require.ErrorIs(t, err, operator.ErrSuperseded)
	assert.Nil(t, certificate, "a caller that lost the lineage receives no certificate")
	assert.Equal(t, operator.Serial("serial-2"), store.current[subject].Serial,
		"the record stays with the successor")

	// The control: the certificate that did carry the lineage renews on,
	// so what refused the call above is the record and not the window.
	_, err = svc.Renew(context.Background(), held(subject, second.Serial, c.at.Add(-lifetime+lifetime/3)))
	require.NoError(t, err)
}

// TestService_RenewRefusesACertificateThatNamesNoOperator keeps the
// agent's certificate on its own route (ADR-0040). The operator beside
// it is what says the refusal is the service type: both certificates
// are inside their window and only one is renewed here.
func TestService_RenewRefusesACertificateThatNamesNoOperator(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, c := service(t, store, issuer)

	issuedAt := c.at.Add(-lifetime + lifetime/4)
	_, err := svc.Renew(context.Background(), held(operatorGRN("acme", "one"), "serial-0", issuedAt))
	require.NoError(t, err)

	_, err = svc.Renew(context.Background(), held(agentGRN("acme", "bot-1"), "serial-0", issuedAt))
	require.ErrorIs(t, err, operator.ErrNotAnOperator)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
}

// TestService_RenewLeavesNoRecordWhenSigningFails holds the order the
// two writes happen in: nothing is recorded for a certificate that was
// never signed, so a failing authority does not cost the operator the
// certificate it still holds.
func TestService_RenewLeavesNoRecordWhenSigningFails(t *testing.T) {
	store, issuer := newRecords(), &signer{}
	svc, c := service(t, store, issuer)
	subject := operatorGRN("acme", "one")

	issuedAt := c.at
	first, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)

	issuer.err = errorx.New(errorx.KindUnavailable, "the authority is unreachable")
	c.at = issuedAt.Add(lifetime - lifetime/3)
	_, err = svc.Renew(context.Background(), held(subject, first.Serial, issuedAt))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
	assert.Equal(t, operator.Serial("serial-1"), store.current[subject].Serial,
		"the certificate the operator holds is still the one that renews")
}
