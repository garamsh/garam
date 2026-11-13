package api_test

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// waitsForDelivery marks an operation whose answer waits for the policy
// service to hold the change (ADR-0165); every other entry says why the
// operation raises no mark.
const waitsForDelivery = "waits"

// feedClassification is every operation that writes, by operationId:
// the two API specs' POST, PUT, PATCH and DELETE operations and the
// sign-in callback, which creates a user. It is the check that a route
// added later is decided: an operation absent from it fails the test
// below until its author has read what its write raises in the outbox
// (ADR-0165 item 1) and said whether it waits.
var feedClassification = map[string]string{
	// api/openapi.yaml, what waits.
	"createOrg": waitsForDelivery, "deleteOrg": waitsForDelivery,
	"revokeAgentSupervisor": waitsForDelivery,
	"createGroup":           waitsForDelivery, "deleteGroup": waitsForDelivery,
	"addGroupMember": waitsForDelivery, "removeGroupMember": waitsForDelivery,
	"createProject": waitsForDelivery, "deleteProject": waitsForDelivery,
	"addProjectMember": waitsForDelivery, "removeProjectMember": waitsForDelivery,
	"updateGroup":      waitsForDelivery + " when the body writes roles",
	"updateProject":    waitsForDelivery + " when the body writes roles",
	"registerPassword": waitsForDelivery, "acceptInvitation": waitsForDelivery,
	"oauthCallback":    waitsForDelivery + " when it created the user",
	"registerOperator": waitsForDelivery, "deleteOperator": waitsForDelivery,
	"writeOperatorDelegation": waitsForDelivery, "endOperatorDelegation": waitsForDelivery,
	"grantOrgConsent": waitsForDelivery, "withdrawOrgConsent": waitsForDelivery,
	// api/openapi.yaml, what raises no mark.
	"loginWithReturnPath": "stores a login state", "logout": "ends a session",
	"issueSessionHandoff": "stores a handoff", "redeemSessionHandoff": "opens a session",
	"loginPassword": "opens a session of a user that exists", "requestDeviceCode": "stores a device grant",
	"deviceToken": "spends a device grant", "decideDeviceCode": "decides a device grant",
	"exchangeSessionToken": "mints a token", "revokeMySession": "ends a session",
	"updateOrg":        "writes orgs.name, which no trigger watches",
	"checkPermissions": "reads", "renameAgent": "writes agents.name, which no trigger watches",
	"approveAgentEnrollment": "stores an approval", "withdrawAgentEnrollment": "ends an approval",
	"updateOperator":   "writes the name and description, which no trigger watches",
	"issueOperatorKey": "writes operator_keys", "revokeOperatorKey": "writes operator_keys",
	"setOperatorManager":           "writes operators.manager, which no trigger watches",
	"setOperatorControlEndpoint":   "writes operators.control_endpoint, which no trigger watches",
	"clearOperatorControlEndpoint": "writes operators.control_endpoint, which no trigger watches",
	"mintOperationAuthority":       "mints a token", "createPolicy": "the policy store is not fed by the outbox",
	"addPolicyVersion": "the policy store is not fed by the outbox", "activatePolicy": "the policy store is not fed by the outbox",
	"deletePolicy": "the policy store is not fed by the outbox", "rollbackPolicy": "the policy store is not fed by the outbox",
	"attachPolicy": "the policy store is not fed by the outbox", "detachPolicy": "the policy store is not fed by the outbox",
	"createInvitation": "writes invitations", "revokeInvitation": "writes invitations",
	// api/machine.yaml, what waits.
	"createManagedAgent": waitsForDelivery, "admitAgentEnrollment": waitsForDelivery + " when it admits a new agent or replaces a supervisor's",
	"enrollStandaloneAgent": waitsForDelivery,
	// api/machine.yaml, what raises no mark.
	"exchangeToken": "mints a token", "exchangeClientAssertion": "mints a token",
	"introspectIssuedToken": "reads", "mintAgentToken": "mints a token",
	"reportProvisioningState": "writes agents.provisioning_state, which no trigger watches",
	"reportRuntimeState":      "writes agents.runtime_state, which no trigger watches",
	"postMessage":             "writes messages", "postOutboxMessage": "writes messages",
	"claimMessage": "writes messages", "renewClaim": "writes messages", "forwardTurnEvent": "forwards",
	"completeClaim": "writes messages", "introspectOperationAuthority": "reads",
	"introspectController": "reads", "activateAgent": "writes agent_executions",
	"deactivateAgent": "writes agent_executions", "introspectAgentExecution": "reads",
	"recoverAgentCredential": "writes agent_executions", "prepareAgentEnrollment": "stores an approval",
}

// TestFeedClassification_NamesEveryOperationThatWrites holds that the
// table above and the specs agree: a written operation the table does
// not name is a route nobody has decided, and an entry naming no
// operation is a decision about a route that is gone.
func TestFeedClassification_NamesEveryOperationThatWrites(t *testing.T) {
	written := map[string]string{}
	for _, spec := range []string{"../../api/openapi.yaml", "../../api/machine.yaml"} {
		raw, err := os.ReadFile(spec)
		require.NoError(t, err)
		var doc struct {
			Paths map[string]map[string]struct {
				OperationID string `yaml:"operationId"`
			} `yaml:"paths"`
		}
		require.NoError(t, yaml.Unmarshal(raw, &doc))
		for path, methods := range doc.Paths {
			for method, op := range methods {
				switch strings.ToUpper(method) {
				case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
					written[op.OperationID] = method + " " + path
				case http.MethodGet:
					if op.OperationID == "oauthCallback" {
						written[op.OperationID] = method + " " + path
					}
				}
			}
		}
	}
	var undecided, gone []string
	for id, route := range written {
		if _, ok := feedClassification[id]; !ok {
			undecided = append(undecided, id+" ("+route+")")
		}
	}
	for id := range feedClassification {
		if _, ok := written[id]; !ok {
			gone = append(gone, id)
		}
	}
	sort.Strings(undecided)
	sort.Strings(gone)
	assert.Empty(t, undecided, "decide whether each waits for the policy service (ADR-0165)")
	assert.Empty(t, gone, "these name no written operation")
}
