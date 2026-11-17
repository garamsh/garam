//go:build policyservice

package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/policyrelay"
	relaygrpc "github.com/garamsh/garam/platform/internal/policyrelay/grpc"
)

// policyRef names a policy of acme as the service's records do.
func policyRef(id string) string {
	return grn.New("acme", "policy", id).String()
}

// auditRecords reads the organization's audit records as cookie and
// answers the status and body.
func (s *relayStack) auditRecords(t *testing.T, cookie *http.Cookie, query string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.origin+"/audit-records"+query, nil)
	require.NoError(t, err)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// TestPolicyRelay_AnOwnerAndAnAdministratorReadTheAuditRecords shows
// the relay (ADR-0176) answering as the signed-in user: the records of
// a policy write and of a denial reach an organization's owner and
// administrator, and a member of it and another organization's owner
// are refused by the service, the reads being the same request. The
// owner's read is the control: the refusals are the reader's and not
// the route's.
func TestPolicyRelay_AnOwnerAndAnAdministratorReadTheAuditRecords(t *testing.T) {
	s := newRelayStack(t, realRelay)
	guardrail := textBody("guardrail", relayGuardrail)
	status, got := s.create(t, s.owner, guardrail)
	require.Equal(t, http.StatusCreated, status, got)
	id := policyID(t, got)
	status, got = s.create(t, s.other, guardrail)
	require.Equal(t, http.StatusForbidden, status, "a member's write is the denial to record: %s", got)

	for name, reader := range map[string]*http.Cookie{"owner": s.owner, "administrator": s.admin} {
		t.Run(name, func(t *testing.T) {
			// A service that has recorded other runs of this organization
			// folds the denial into an older record, so the read follows
			// the pages until it has both.
			var records []apigen.AuditRecord
			query := "?pageSize=100"
			for {
				status, got := s.auditRecords(t, reader, query)
				require.Equal(t, http.StatusOK, status, got)
				var page apigen.AuditRecordPage
				require.NoError(t, json.Unmarshal([]byte(got), &page))
				records = append(records, page.Records...)
				if page.NextPageToken == nil || len(records) >= 1000 {
					break
				}
				query = "?pageSize=100&pageToken=" + *page.NextPageToken
			}
			page := apigen.AuditRecordPage{Records: records}

			var write, denial *apigen.AuditRecord
			for i, r := range page.Records {
				switch {
				case r.Kind == apigen.Write && len(r.PolicyIds) == 1 && r.PolicyIds[0] == policyRef(id):
					write = &page.Records[i]
				case r.Kind == apigen.Denial && r.Subject == "grn:root/iam/user/bob":
					denial = &page.Records[i]
				}
			}
			require.NotNil(t, write, "the owner's write is recorded: %s", got)
			assert.Equal(t, s.ownerGRN.String(), write.Subject, "who wrote")
			assert.Equal(t, "created", write.Outcome)
			assert.NotEmpty(t, write.Action)
			assert.Equal(t, policyRef(id), write.Resource)
			assert.Equal(t, int32(1), write.VersionTo, "the step and version of a write")
			assert.Equal(t, int64(1), write.Count)
			require.NotNil(t, denial, "the member's refusal is recorded: %s", got)
			assert.Equal(t, "deny", denial.Outcome)
			assert.NotEmpty(t, denial.Action)
			assert.GreaterOrEqual(t, denial.Count, int64(1), "the count of a collapsed denial")
			assert.False(t, denial.FirstAt.IsZero(), "the time")
			for i := 1; i < len(page.Records); i++ {
				assert.Less(t, page.Records[i].Id, page.Records[i-1].Id, "newest first")
			}
		})
	}

	status, got = s.auditRecords(t, s.other, "")
	assert.Equal(t, http.StatusForbidden, status, "a member holds no iam:audit.read: %s", got)
	assert.NotContains(t, got, id, "a refusal tells nothing of the records")
	status, got = s.auditRecords(t, s.rival, "")
	assert.Equal(t, http.StatusForbidden, status, "another organization's owner is no reader of this one: %s", got)
	assert.NotContains(t, got, id)

	status, got = s.auditRecords(t, s.owner, "")
	assert.Equal(t, http.StatusOK, status, "control: the same read by the owner is answered: %s", got)
}

// TestPolicyRelay_TheAuditRecordsArePaged reads the records a page at a
// time, newest first, with the token each page answers.
func TestPolicyRelay_TheAuditRecordsArePaged(t *testing.T) {
	s := newRelayStack(t, realRelay)
	for range 2 {
		status, got := s.create(t, s.owner, textBody("guardrail", relayGuardrail))
		require.Equal(t, http.StatusCreated, status, got)
	}

	status, got := s.auditRecords(t, s.owner, "?pageSize=1")
	require.Equal(t, http.StatusOK, status, got)
	var first apigen.AuditRecordPage
	require.NoError(t, json.Unmarshal([]byte(got), &first))
	require.Len(t, first.Records, 1)
	require.NotNil(t, first.NextPageToken, "more records follow")

	status, got = s.auditRecords(t, s.owner, "?pageSize=1&pageToken="+*first.NextPageToken)
	require.Equal(t, http.StatusOK, status, got)
	var second apigen.AuditRecordPage
	require.NoError(t, json.Unmarshal([]byte(got), &second))
	require.Len(t, second.Records, 1)
	assert.Less(t, second.Records[0].Id, first.Records[0].Id, "the next page is older")

	status, got = s.auditRecords(t, s.owner, "?pageToken=not-a-token")
	assert.Equal(t, http.StatusBadRequest, status, "a token the service never issued: %s", got)
}

// TestPolicyRelay_TheAuditRecordsOfAServiceThatIsDownAreUnavailable
// points the relay at an address nothing listens on. The same read of
// the real service is the control.
func TestPolicyRelay_TheAuditRecordsOfAServiceThatIsDownAreUnavailable(t *testing.T) {
	down := newRelayStack(t, func(*realPolicy) policyrelay.Service {
		conn, err := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		return relaygrpc.New(policyv1.NewPolicyServiceClient(conn))
	})
	status, got := down.auditRecords(t, down.owner, "")
	assert.Equal(t, http.StatusServiceUnavailable, status, got)

	up := newRelayStack(t, realRelay)
	status, got = up.auditRecords(t, up.owner, "")
	assert.Equal(t, http.StatusOK, status, "control: the service answers: %s", got)
}
