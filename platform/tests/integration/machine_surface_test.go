package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/ca"
	"github.com/garamsh/garam/platform/internal/message"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// detachPolicy removes policyGRN's attachment to subject through the
// lifecycle service.
func detachPolicy(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string, subject grn.GRN) {
	t.Helper()
	_, err := srv.DetachPolicy(context.Background(), &authzv1.DetachPolicyRequest{
		PolicyGrn:  policyGRN,
		SubjectGrn: subject.String(),
	})
	require.NoError(t, err)
}

// detachRefused asserts the lifecycle refuses to remove policyGRN's
// attachment to subject: it is an active policy's last.
func detachRefused(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string, subject grn.GRN) {
	t.Helper()
	_, err := srv.DetachPolicy(context.Background(), &authzv1.DetachPolicyRequest{
		PolicyGrn:  policyGRN,
		SubjectGrn: subject.String(),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// deletePolicy removes policyGRN through the lifecycle service, which
// takes it out of every check.
func deletePolicy(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string) {
	t.Helper()
	_, err := srv.DeletePolicy(context.Background(), &authzv1.DeletePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
}

// TestMachineSurface_AnAgentActsOnItsOwnQueueAndNoOther carries a
// message end to end over the listener an agent authenticates on: one
// agent posts, the addressed agent claims it, holds it while it works,
// and reports it delivered, after which the message leaves the queue.
//
// Every refusal sits beside the same call made by the agent the message
// is addressed to. Both callers authenticate, both hold certificates
// this listener verifies, and only one of them is the recipient — so
// what refuses is the one comparison rather than a surface that refuses
// whoever asks.
func TestMachineSurface_AnAgentActsOnItsOwnQueueAndNoOther(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)

	url := machineListener(t, p, sealer)
	operator := operatorClient(t, p, url, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	senderGRN, sender := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	queue := url + "/agents/" + pathEscape(recipientGRN.String()) + "/claims"

	submission := `{"recipient":"` + recipientGRN.String() + `","idempotencyKey":"key-1","body":"hello"}`
	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submission)
	require.Equal(t, http.StatusCreated, status, string(body))
	var posted machinegen.Message
	require.NoError(t, json.Unmarshal(body, &posted))
	assert.Equal(t, senderGRN.String(), posted.Sender, "the sender is what the certificate named")
	assert.Equal(t, recipientGRN.String(), posted.Recipient)

	// A sender that could not read the answer sends it again, and one
	// message is stored.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submission)
	require.Equal(t, http.StatusCreated, status, string(body))
	var repeated machinegen.Message
	require.NoError(t, json.Unmarshal(body, &repeated))
	assert.Equal(t, posted.Id, repeated.Id, "a repeated key answers with the message already stored")

	status, body = machineSend(t, sender, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	assert.Contains(t, string(body), `"kind":"permission_denied"`)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, posted.Id, claimed.Message.Id)
	assert.Equal(t, "hello", claimed.Message.Body)
	assert.Equal(t, 1, claimed.Attempt)
	assert.True(t, claimed.ExpiresAt.After(time.Now()), "a hold that had lapsed before it was answered")

	held := queue + "/" + claimed.Id
	status, body = machineSend(t, sender, http.MethodPost, held+"/renewal", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	status, body = machineSend(t, sender, http.MethodPost, held+"/completion", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))

	// The holder keeps its claim while the turn runs.
	status, body = machineSend(t, recipient, http.MethodPost, held+"/renewal", "")
	require.Equal(t, http.StatusOK, status, string(body))
	var renewed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &renewed))
	assert.Equal(t, claimed.Id, renewed.Id, "renewal keeps the hold rather than taking a new one")
	assert.True(t, renewed.ExpiresAt.After(claimed.ExpiresAt), "renewal left the hold where it started")

	status, body = machineSend(t, recipient, http.MethodPost, held+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))
	// A holder that could not read the answer reports it again, and the
	// same delivery comes back rather than a refusal.
	status, body = machineSend(t, recipient, http.MethodPost, held+"/completion", "")
	assert.Equal(t, http.StatusNoContent, status, string(body))

	// The delivered message is handed out no more, so the next poll
	// waits its wait out and answers that there is nothing.
	start := time.Now()
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body))
	assert.GreaterOrEqual(t, time.Since(start), briefDelivery().Wait,
		"the poll answered an empty queue before its wait ran out")

	// A message addressed to an agent garam has never provisioned is
	// stored and answered with success: rejecting it would destroy the
	// message and tell the sender nothing (ADR-0013). It is in the queue
	// rather than merely answered for, and storing it provisioned
	// nothing.
	ghost := scopedGRN("acme", agent.ServiceType, "0000000000000000")
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages",
		`{"recipient":"`+ghost.String()+`","idempotencyKey":"key-2","body":"nobody is home"}`)
	require.Equal(t, http.StatusCreated, status, string(body))

	stranded, err := messagepg.New(p).Claim(ctx, ghost, time.Minute, 1, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "nobody is home", stranded.Message.Body)
	_, err = agentpg.New(p).Get(ctx, ghost)
	assert.ErrorIs(t, err, agent.ErrAgentNotFound, "storing the message provisioned an agent")
}

// TestMachineSurface_AnotherOrganizationsAgentReachesNothingOnThisQueue
// carries the same message path across an organization boundary. A
// third agent is provisioned by another organization's operator and
// authenticates on the same listener against its own authority, then
// makes the calls the recipient makes. Each refusal sits beside the
// recipient's own call to that endpoint: the stranger's certificate
// authenticates here, so what refuses is the recipient comparison
// rather than a certificate this listener rejected.
//
// Its post crossed until ADR-0024. What an identity may send is now
// decided by policy inside its organization, and across the boundary
// by the policy service (ADR-0168): where it does not allow, the
// message is not stored to the recipient, and the refusal comes
// back to the sender as an ordinary inbound message (ADR-0013) rather
// than as a status.
func TestMachineSurface_AnotherOrganizationsAgentReachesNothingOnThisQueue(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	for _, name := range []string{"acme", "globex"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)

	policies, _ := policyEngine(t, p)
	url := machineListener(t, p, sealer)
	acmeOperator := operatorClient(t, p, url, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	globexOperator := operatorClient(t, p, url, scopedGRN("globex", agent.ServiceTypeOperator, "one"))
	senderGRN, sender := provisioned(t, url, acmeOperator)
	recipientGRN, recipient := provisioned(t, url, acmeOperator)
	strangerGRN, stranger := provisioned(t, url, globexOperator)
	require.Equal(t, "globex", strangerGRN.Organization, "the stranger was minted in the operator's organization")
	queue := url + "/agents/" + pathEscape(recipientGRN.String()) + "/claims"

	status, body := machineSend(t, sender, http.MethodPost, url+"/messages",
		`{"recipient":"`+recipientGRN.String()+`","idempotencyKey":"key-1","body":"hello"}`)
	require.Equal(t, http.StatusCreated, status, string(body))
	var posted machinegen.Message
	require.NoError(t, json.Unmarshal(body, &posted))

	status, body = machineSend(t, stranger, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	assert.Contains(t, string(body), `"kind":"permission_denied"`)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, posted.Id, claimed.Message.Id)
	assert.Equal(t, senderGRN.String(), claimed.Message.Sender, "the sender the certificate named survived the trip")
	assert.Equal(t, "hello", claimed.Message.Body)

	held := queue + "/" + claimed.Id
	status, body = machineSend(t, stranger, http.MethodPost, held+"/renewal", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	status, body = machineSend(t, stranger, http.MethodPost, held+"/completion", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))

	status, body = machineSend(t, recipient, http.MethodPost, held+"/renewal", "")
	require.Equal(t, http.StatusOK, status, string(body))

	// Completing the first message empties the queue, so a message the
	// stranger managed to store would be the next one handed out.
	status, body = machineSend(t, recipient, http.MethodPost, held+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))

	// The post crosses no further than the rest: the service allows
	// nothing between the two organizations here, so nothing is stored
	// to the recipient — and the refusal is not a 4xx but an ordinary
	// inbound message to the sender, which the 201 below answers
	// (ADR-0013, ADR-0168).
	status, body = machineSend(t, stranger, http.MethodPost, url+"/messages",
		submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusCreated, status, string(body))
	var refusal machinegen.Message
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, strangerGRN.String(), refusal.Recipient, "the refusal came back to the sender")
	assert.Equal(t, recipientGRN.String(), refusal.Sender, "written under the intended recipient's identity")

	// It sits in the sender's own queue like any other message.
	strangerQueue := url + "/agents/" + pathEscape(strangerGRN.String()) + "/claims"
	status, body = machineSend(t, stranger, http.MethodPost, strangerQueue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, refusal.Id, claimed.Message.Id)
	assert.Equal(t, refusalText(recipientGRN), claimed.Message.Body)
	status, body = machineSend(t, stranger, http.MethodPost, strangerQueue+"/"+claimed.Id+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))

	// No policy the sender's own organization writes carries it across
	// either, because the service holds the boundary (ADR-0168). The
	// Allow below matches every resource there is and buys nothing.
	reachAll := activePolicy(t, policies, orgRootGRN("acme"), "reach-everything",
		statementDoc("reach-everything", wire.EffectAllow, message.ActionSend, "grn:*/*"))
	attachPolicy(t, policies, reachAll, senderGRN)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages",
		submissionTo(strangerGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, senderGRN.String(), refusal.Recipient, "the wildcard policy did not carry the message across")

	// That same Allow is active, attached and matching inside acme, so
	// what refused above is the recipient's organization and not a
	// policy that reaches nothing.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages",
		submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Nothing refused reached the queue, so the one message waiting is
	// the one that was accepted.
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, senderGRN.String(), claimed.Message.Sender)
	assert.Equal(t, "key-4", claimed.Message.IdempotencyKey)
}

// TestMachineSurface_ADeniedSendStoresNothingForItsRecipient carries
// what the machine listener does with a policy service's denial inside
// the sender's organization (ADR-0024, ADR-0168), against real
// Postgres. Which policy denies a send, and whom it attaches to, is the service's
// and is held by its own suites; the service here is a double that
// denies one pair once told to.
//
// Three controls sit against the refusal, because a 403 holds as
// readily for a surface that refuses everything: the same sender
// reaching a recipient the denial does not name, the agent beside it
// reaching the one it does, and the same call before the denial. The
// queue is drained at the end, so what was refused is shown to be
// absent rather than merely unanswered.
func TestMachineSurface_ADeniedSendStoresNothingForItsRecipient(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)

	decisions := &sendsRefusing{}
	url := machineListenerDeciding(t, p, sealer, nil, decisions)
	operator := operatorClient(t, p, url, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	senderGRN, sender := provisioned(t, url, operator)
	_, other := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	siblingGRN, _ := provisioned(t, url, operator)

	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))

	decisions.deny(senderGRN, recipientGRN)

	// The service refused this one, which is a different answer from
	// the boundary refusal and says so in the body.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, string(body))

	// The denial names one recipient, so the sender still reaches the
	// agent beside it.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(siblingGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The denial names one sender, so the agent beside it reaches the
	// recipient the sender may not.
	status, body = machineSend(t, other, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// What the recipient's queue holds is every accepted message and no
	// refused one: a policy refusal stores nothing, where a message
	// garam merely cannot route is stored (ADR-0013).
	queue := url + "/agents/" + pathEscape(recipientGRN.String()) + "/claims"
	var keys []string
	for range 2 {
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		keys = append(keys, claimed.Message.IdempotencyKey)
		status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{"key-1", "key-4"}, keys)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a refused message reached the queue")
}

// TestMachineSurface_ARecipientsOwnPolicyAdmitsNoForeignSender covers
// the boundary a resource pattern cannot hold (#265). The policies of
// the organization owning the recipient apply to every subject of the
// check unless attached, so one unattached Allow in the recipient's
// organization would grant every sender anywhere. The surface's own
// half is held here: a foreign sender's post is not stored to the
// recipient, whatever the recipient's organization has written, and
// comes back to the sender as an ordinary inbound message (ADR-0013).
// What decides it is the policy service's answer (ADR-0168), which
// this suite stands a double in for; the engine's scope check that
// ADR-0038 added is held by the authz suites, and the service's own
// boundary by the policy matrix.
//
// The recipient's own neighbour is the control: it is stored while the
// foreign sender's post is refused, so a surface that stopped accepting
// posts fails it.
func TestMachineSurface_ARecipientsOwnPolicyAdmitsNoForeignSender(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	for _, name := range []string{"alpha", "beta"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)

	policies, _ := policyEngine(t, p)
	url := machineListener(t, p, sealer)
	alphaOperator := operatorClient(t, p, url, scopedGRN("alpha", agent.ServiceTypeOperator, "one"))
	betaOperator := operatorClient(t, p, url, scopedGRN("beta", agent.ServiceTypeOperator, "one"))
	foreignGRN, foreign := provisioned(t, url, alphaOperator)
	recipientGRN, recipient := provisioned(t, url, betaOperator)
	neighbourGRN, neighbour := provisioned(t, url, betaOperator)
	require.Equal(t, "alpha", foreignGRN.Organization)
	require.Equal(t, "beta", recipientGRN.Organization)

	// With nothing written anywhere, the foreign sender's message is
	// not stored to the recipient — it comes back to the sender as an
	// ordinary inbound message — and the recipient's own neighbour is
	// stored as before.
	status, body := machineSend(t, foreign, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))
	var refusal machinegen.Message
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, foreignGRN.String(), refusal.Recipient, "the refusal came back to the sender, not to the foreign recipient")
	status, body = machineSend(t, neighbour, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The recipient's organization activates one wildcard policy and
	// attaches it to nobody, which is what an administrator does by
	// detaching the last attachment from one they had aimed at a
	// subject. The surface does not store the foreign post anyway.
	// What comes back names neither a policy nor a consent: this
	// refusal is not one beta wrote and not one it can lift.
	openDoor := activePolicy(t, policies, orgRootGRN("beta"), "everything",
		statementDoc("everything", wire.EffectAllow, "*", "grn:beta/*"))
	status, body = machineSend(t, foreign, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, foreignGRN.String(), refusal.Recipient)
	assert.Equal(t, refusalText(recipientGRN), refusal.Body)

	// The same call from inside the organization is accepted while that
	// policy is active, so what turns the foreign one back is the
	// missing relation and not a surface that stopped accepting
	// messages.
	status, body = machineSend(t, neighbour, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Deleting the policy changes nothing about the foreign sender: it
	// came back before the policy existed and comes back after, so the
	// refusal never depended on what beta wrote.
	deletePolicy(t, policies, openDoor)
	status, body = machineSend(t, foreign, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-5"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, foreignGRN.String(), refusal.Recipient)

	// Nothing the foreign sender posted is in the recipient's queue.
	queue := url + "/agents/" + pathEscape(recipientGRN.String()) + "/claims"
	var senders []string
	for range 2 {
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		senders = append(senders, claimed.Message.Sender)
		status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{neighbourGRN.String(), neighbourGRN.String()}, senders)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a foreign sender's message reached the queue")

	// Every one of the foreign sender's posts is in its own queue
	// instead: three refusals, one per post, each written under the
	// intended recipient's identity.
	foreignQueue := url + "/agents/" + pathEscape(foreignGRN.String()) + "/claims"
	var refusals []string
	for range 3 {
		status, body = machineSend(t, foreign, http.MethodPost, foreignQueue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		assert.Equal(t, recipientGRN.String(), claimed.Message.Sender)
		assert.Equal(t, refusalText(recipientGRN), claimed.Message.Body)
		refusals = append(refusals, claimed.Message.IdempotencyKey)
		status, body = machineSend(t, foreign, http.MethodPost, foreignQueue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{
		"crossing-refused:" + foreignGRN.String() + ":key-1",
		"crossing-refused:" + foreignGRN.String() + ":key-3",
		"crossing-refused:" + foreignGRN.String() + ":key-5",
	}, refusals)
}
