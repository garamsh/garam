package api_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// enginePolicyManager is the API's policy client over a real engine
// policy service, whose store is never reached: a document the engine
// refuses is refused before it.
func enginePolicyManager(t *testing.T) *api.GRPCPolicyManager {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	authzv1.RegisterPolicyServiceServer(srv, authz.NewPolicyGRPCServer(nil, nil))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return api.NewGRPCPolicyManager(conn)
}

// TestCreatePolicy_AnswersTheEnginesReasonAndPointer takes the refusal
// a real engine gives a document over gRPC and serves it through the
// create route, so the code and the pointer are read off the HTTP body
// the console reads. The engine's refusal of a document without a
// Resource is the case; its status and kind are the ones every
// document refusal shares, so only the reason tells it apart.
func TestCreatePolicy_AnswersTheEnginesReasonAndPointer(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyCreate})
	_, p := policyFixture(slug)

	document := json.RawMessage(`{"Version":"2026-07-25","Statement":[{"Sid":"s","Effect":"Deny","Action":["org:delete"]}]}`)
	_, refusal := enginePolicyManager(t).CreatePolicy(testCtx(t), p.OrgGRN, "read-only", document, ownerGRN)
	require.Error(t, refusal, "the engine refuses the document")
	f.policies.EXPECT().
		CreatePolicy(mock.Anything, p.OrgGRN, "read-only", document, ownerGRN).
		Return(nil, refusal)

	rec := f.do(t, "POST", "/orgs/"+slug+"/policies", `{"name":"read-only","document":`+string(document)+`}`, owner)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody[apigen.Error](t, rec)
	assert.Equal(t, "invalid_argument", body.Kind)
	require.NotNil(t, body.Reason)
	assert.Equal(t, authz.ReasonPolicyDocumentResourceMissing, *body.Reason)
	require.NotNil(t, body.Path)
	assert.Equal(t, "/Statement/0/Resource", *body.Path)
}

// TestInvitationRefusals_SayWhichRefusalTheyWere reads the two
// refusals an invitation answers with the same status and kind, on
// both routes that spend one, and finds them told apart by reason. An
// unknown token is the control: a refusal no rule needs to explain
// carries no reason at all.
func TestInvitationRefusals_SayWhichRefusalTheyWere(t *testing.T) {
	f := newFixture(t)
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")
	invitedOrg(t, f, "used-tok", time.Now().UTC().Add(time.Hour))
	require.Equal(t, http.StatusOK, f.do(t, "POST", "/invitations/used-tok/accept", "", invitee).Code)
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t),
		org.NewInvitation("old-tok", orgGRNOf("acme-robotics"), org.RoleMember, creator, time.Now().UTC().Add(-time.Minute))))

	// A second person, so the used token is refused for being spent
	// rather than for a membership the first already holds.
	second := f.sessionCookieFor(t, "second-sub", "Second")
	for _, tc := range []struct {
		name, method, path, body, reason string
	}{
		{"accept, used", "POST", "/invitations/used-tok/accept", "", org.ReasonInvitationAccepted},
		{"accept, expired", "POST", "/invitations/old-tok/accept", "", org.ReasonInvitationExpired},
		{"register, expired", "POST", "/auth/password/register", registerBody("old-tok", "bob", registeredPassword), org.ReasonInvitationExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.do(t, tc.method, tc.path, tc.body, second)
			require.Equal(t, http.StatusConflict, rec.Code)
			body := decodeBody[apigen.Error](t, rec)
			assert.Equal(t, "failed_precondition", body.Kind)
			require.NotNil(t, body.Reason)
			assert.Equal(t, tc.reason, *body.Reason)
			assert.Nil(t, body.Path)
		})
	}

	unknown := decodeBody[apigen.Error](t, f.do(t, "POST", "/invitations/nope/accept", "", invitee))
	assert.Equal(t, "not_found", unknown.Kind)
	assert.Nil(t, unknown.Reason)
}
