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

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
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
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)

	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	queue := url + "/agents/" + recipientGRN.String() + "/claims"

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

	stranded, err := message.NewPostgresStore(p).Claim(ctx, ghost, time.Minute, 1, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "nobody is home", stranded.Message().Body())
	_, err = agent.NewPostgresStore(p).Get(ctx, ghost)
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
// by the consent relation alone (ADR-0035): where no relation stands
// the message is not stored to the recipient, and the refusal comes
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
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	acmeOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	globexOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("globex", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, acmeOperator)
	recipientGRN, recipient := provisioned(t, url, acmeOperator)
	strangerGRN, stranger := provisioned(t, url, globexOperator)
	require.Equal(t, "globex", strangerGRN.Organization, "the stranger was minted in the operator's organization")
	queue := url + "/agents/" + recipientGRN.String() + "/claims"

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

	// The post crosses no further than the rest: no consent stands
	// between the two organizations, so nothing is stored to the
	// recipient — and the refusal is not a 4xx but an ordinary inbound
	// message to the sender, which the 201 below answers (ADR-0013,
	// ADR-0035).
	status, body = machineSend(t, stranger, http.MethodPost, url+"/messages",
		submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusCreated, status, string(body))
	var refusal machinegen.Message
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, strangerGRN.String(), refusal.Recipient, "the refusal came back to the sender")
	assert.Equal(t, recipientGRN.String(), refusal.Sender, "written under the intended recipient's identity")

	// It sits in the sender's own queue like any other message.
	strangerQueue := url + "/agents/" + strangerGRN.String() + "/claims"
	status, body = machineSend(t, stranger, http.MethodPost, strangerQueue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, refusal.Id, claimed.Message.Id)
	assert.Contains(t, claimed.Message.Body, "do not both consent")
	status, body = machineSend(t, stranger, http.MethodPost, strangerQueue+"/"+claimed.Id+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))

	// No policy the sender's own organization writes carries it across
	// either, because a policy is never asked across the boundary —
	// the consent relation is the only thing that opens it (ADR-0024,
	// ADR-0035). The Allow below matches every resource there is and
	// buys nothing.
	reachAll := activePolicy(t, policies, orgRootGRN("acme"), "reach-everything",
		statementDoc("reach-everything", wire.EffectAllow, message.ActionSend, "grn:*:*"))
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

// TestMachineSurface_AnAttachedPolicyDecidesWhatAnAgentSends carries
// ADR-0024 end to end. An organization writes a policy through the
// lifecycle service, attaches it to one of its agents, and the machine
// listener asks the engine over gRPC before it stores anything — the
// two processes `garam serve authz` and `garam serve machine` are, over
// one database.
//
// Three controls sit against the refusals, because a 403 holds as
// readily for a surface that refuses everything: the same sender
// reaching a recipient the Deny does not name, the agent beside it that
// holds no policy reaching the one it does, and the same call once the
// policy is deleted. The queue is drained at the end, so what was
// refused is shown to be absent rather than merely unanswered.
//
// The detach step in the middle is the one an operator runs to stop
// restricting the sender. It is refused — the sender's attachment is
// the policy's last, and removing it would apply the Deny to the whole
// organization — so the policy goes on deciding for its subject alone,
// and the agent it was never attached to is never reached.
func TestMachineSurface_AnAttachedPolicyDecidesWhatAnAgentSends(t *testing.T) {
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
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	_, other := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	siblingGRN, _ := provisioned(t, url, operator)

	// An agent nobody has written a policy for reaches its own
	// organization, so the surface starts by accepting the call the
	// policy below refuses.
	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))

	denied := activePolicy(t, policies, acme, "no-talking",
		statementDoc("no-talking", wire.EffectDeny, message.ActionSend, recipientGRN.String()))
	attachPolicy(t, policies, denied, senderGRN)

	// The organization's own rule refused this one, which is a different
	// answer from the boundary refusal above and says so in the body.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, string(body))

	// The Deny names one recipient, so the sender still reaches the
	// agent beside it.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(siblingGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The attachment names one subject, so the agent beside the sender
	// reaches the recipient the sender may not.
	status, body = machineSend(t, other, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Detaching the policy's last attachment is refused rather than
	// widening the Deny to the whole org: the agent beside the sender
	// goes on reaching the recipient, and the sender goes on being
	// refused, so the refusal changed nothing the policy decides.
	detachRefused(t, policies, denied, senderGRN)
	status, body = machineSend(t, other, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-5"))
	require.Equal(t, http.StatusCreated, status, string(body))
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-6"))
	require.Equal(t, http.StatusForbidden, status, string(body))

	// Deleting is what takes the policy out of every check, so the
	// refusals above were this policy and nothing beside it.
	deletePolicy(t, policies, denied)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-7"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// What the recipient's queue holds is every accepted message and no
	// refused one: a policy refusal stores nothing, where a message
	// garam merely cannot route is stored (ADR-0013).
	queue := url + "/agents/" + recipientGRN.String() + "/claims"
	var keys []string
	for range 4 {
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		keys = append(keys, claimed.Message.IdempotencyKey)
		status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{"key-1", "key-4", "key-5", "key-7"}, keys)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a refused message reached the queue")
}

// TestMachineSurface_AGroupAttachedPolicyDecidesWhatItsMembersSend
// carries the control the fix turns on against real Postgres and the
// real engine: one policy document, one sender, one recipient, and only
// the attachment target moving. A group is a target an administrator
// can pick and an agent is a member of one the way a user is
// (ADR-0008, ADR-0014), so attached to the sender's group and attached
// to the sender itself the call has to end the same way. It did not:
// the group attachment answered 201 and the direct one 403, which is an
// attachment that reads as a control that is not there.
//
// The agent in no group makes the same call at every step, so what
// refuses is the membership rather than a surface that refuses whoever
// asks.
func TestMachineSurface_AGroupAttachedPolicyDecidesWhatItsMembersSend(t *testing.T) {
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
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	_, outsider := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	siblingGRN, _ := provisioned(t, url, operator)

	engineering := scopedGRN("acme", org.ServiceTypeGroup, "engineering")
	require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(engineering, "engineering", acme)))
	require.NoError(t, orgs.AddGroupMember(ctx, engineering, senderGRN))
	members, err := orgs.ListGroupMembers(ctx, engineering)
	require.NoError(t, err)
	require.Equal(t, []grn.GRN{senderGRN}, members, "the group holds one of the two senders")

	// An agent nobody has written a policy for reaches its own
	// organization, so the surface starts by accepting the call the
	// group attachment below refuses.
	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))

	denied := activePolicy(t, policies, acme, "no-talking",
		statementDoc("no-talking", wire.EffectDeny, message.ActionSend, recipientGRN.String()))
	attachPolicy(t, policies, denied, engineering)

	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, string(body))

	// The Deny names one recipient, so the member reaches the agent
	// beside it.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(siblingGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The agent that is in no group is answered under the same policy.
	status, body = machineSend(t, outsider, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The same document attached to the sender itself refuses the same
	// call. Attaching before detaching keeps the policy attached to
	// something throughout: an active policy's last attachment refuses
	// to detach rather than widening the policy to the whole
	// organization.
	attachPolicy(t, policies, denied, senderGRN)
	detachPolicy(t, policies, denied, engineering)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-5"))
	require.Equal(t, http.StatusForbidden, status, string(body))

	// The agent outside the group is answered here too, so what each
	// attachment refused is the subject it named.
	status, body = machineSend(t, outsider, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-6"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Deleting is what takes the policy out of every check, so both
	// refusals were this document and nothing beside it.
	deletePolicy(t, policies, denied)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-7"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// What the recipient's queue holds is every accepted message and no
	// refused one.
	queue := url + "/agents/" + recipientGRN.String() + "/claims"
	var keys []string
	for range 4 {
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		keys = append(keys, claimed.Message.IdempotencyKey)
		status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{"key-1", "key-4", "key-6", "key-7"}, keys)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a refused message reached the queue")
}

// TestMachineSurface_ARecipientsOwnPolicyAdmitsNoForeignSender covers
// the boundary a resource pattern cannot hold (#265). The engine
// resolves the policies of the organization owning the object, and an
// active policy with no attachment applies to every subject of that
// check — so one unattached Allow in the recipient's organization
// would grant every sender anywhere, whatever the sender's own
// statements say. ADR-0038 closed that: the engine refuses the
// cross-org call before any policy is read, regardless of what beta
// has written, and the listener's consent check (ADR-0035) keeps the
// surface refusing the post anyway.
//
// The policy here is `Action: ["*"]` rather than `message:send`,
// because that is the case that needs no new document: an organization
// holding a wildcard policy written before this action existed would
// be exposed the day the engine's scope check is removed.
//
// The engine is asked the same question directly, twice: it refuses
// the foreign sender before the policy exists and refuses it after.
// That pair is what makes this a test of the engine's boundary rather
// than of a policy that reached nothing, and an engine stuck open
// fails the first half rather than passing both. The engine refuses
// and the surface still does not store the message to the recipient:
// with ADR-0035 the boundary is the consent relation's, not a
// policy's, and where it does not stand the post returns to the sender
// as an ordinary inbound message (ADR-0013).
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
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, engine := policyEngine(t, p)
	evaluator := machineapi.NewGRPCEvaluator(engine)
	url := machineListener(t, p, sealer, engine)
	alphaOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("alpha", agent.ServiceTypeOperator, "one")))
	betaOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("beta", agent.ServiceTypeOperator, "one")))
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

	// The engine refuses the foreign subject before the policy exists:
	// the scope check (ADR-0038) refuses on subject_org alone, without
	// reading any policy. The pair below asks again after beta has
	// written an unattached wildcard Allow, and the answer is the
	// same: the engine holds the boundary here, not the policy. An
	// engine stuck open answers allow the second time and fails the
	// pair.
	//
	// The statement is the one the listener collects for a sender
	// (`sendingStatements`), aimed at the recipient's own organization
	// so that it would allow the send if the engine ever read it. What
	// refuses is therefore the scope check and not a missing grant.
	sending := []machineapi.Statement{{
		Sid:      "machine-own-org",
		Effect:   wire.EffectAllow,
		Action:   []string{message.ActionSend},
		Resource: []string{"grn:" + recipientGRN.Organization + ":*"},
	}}
	crossing := machineapi.EvalInput{
		Subject:    foreignGRN,
		Object:     recipientGRN,
		Action:     message.ActionSend,
		Statements: sending,
		SubjectOrg: orgRootGRN(foreignGRN.Organization),
	}
	allowed, err := evaluator.Evaluate(ctx, crossing)
	require.Error(t, err, "the engine allowed a foreign subject no policy had granted")
	// By kind, not by presence: an unset SubjectOrg fails the same call
	// as KindInvalidArgument at the transport boundary, without ever
	// reaching the scope check this test is about.
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied),
		"refused by the scope check (ADR-0038), not by parsing the request")
	require.False(t, allowed)

	// The control, differing from the question above in the subject and
	// the organization it asserts and in nothing else: from inside the
	// recipient's own organization the same statement is read and the
	// answer is allow. A boundary refusing every call, or a transport
	// refusing the input, fails here instead of passing.
	local := crossing
	local.Subject = neighbourGRN
	local.SubjectOrg = orgRootGRN(recipientGRN.Organization)
	allowed, err = evaluator.Evaluate(ctx, local)
	require.NoError(t, err)
	require.True(t, allowed, "a subject of the object's own organization is allowed by the same statement")

	// The recipient's organization activates one wildcard policy and
	// attaches it to nobody, which is what an administrator does by
	// detaching the last attachment from one they had aimed at a subject.
	openDoor := activePolicy(t, policies, orgRootGRN("beta"), "everything",
		statementDoc("everything", wire.EffectAllow, "*", "grn:beta:*"))

	// That policy does not reach the foreign subject: the same
	// question, refused the same way now that beta has written one
	// document, because the scope check fires before the policy is
	// read.
	allowed, err = evaluator.Evaluate(ctx, crossing)
	require.Error(t, err, "the unattached wildcard policy reached a foreign subject")
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied),
		"refused by the scope check (ADR-0038), not by parsing the request")
	require.False(t, allowed)

	// The surface does not store it anyway, which is the whole of the
	// fix. What comes back names the missing consent rather than a
	// policy: this refusal is not one beta wrote and not one it can
	// lift — only the two organizations' relation opens the boundary.
	status, body = machineSend(t, foreign, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, foreignGRN.String(), refusal.Recipient)
	assert.Contains(t, refusal.Body, "do not both consent")

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
	queue := url + "/agents/" + recipientGRN.String() + "/claims"
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
	foreignQueue := url + "/agents/" + foreignGRN.String() + "/claims"
	var refusals []string
	for range 3 {
		status, body = machineSend(t, foreign, http.MethodPost, foreignQueue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		assert.Equal(t, recipientGRN.String(), claimed.Message.Sender)
		assert.Contains(t, claimed.Message.Body, "do not both consent")
		refusals = append(refusals, claimed.Message.IdempotencyKey)
		status, body = machineSend(t, foreign, http.MethodPost, foreignQueue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{
		"consent-refused:" + foreignGRN.String() + ":key-1",
		"consent-refused:" + foreignGRN.String() + ":key-3",
		"consent-refused:" + foreignGRN.String() + ":key-5",
	}, refusals)
}

// TestMachineSurface_AConsentRelationCarriesAMessageAcrossTheBoundary
// carries ADR-0035 end to end over the real listener: a message
// between two organizations is stored and claimable while the relation
// stands, and refused back to its sender as an ordinary inbound
// message the moment either half is withdrawn. The grants and
// withdraws are written through the store, as the api server's
// handlers write them.
//
// The controls sit beside the crossing: one half alone carries
// nothing, a relation between two other organizations admits nothing
// for this pair, and the same-organization post is unaffected
// throughout — so what opens and closes the boundary here is the
// relation and nothing else.
func TestMachineSurface_AConsentRelationCarriesAMessageAcrossTheBoundary(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	for _, name := range []string{"acme", "globex", "initech"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)

	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	acmeOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	globexOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("globex", agent.ServiceTypeOperator, "one")))
	initechOperator := machineClient(t, installed(t, p, outOfBand, scopedGRN("initech", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, acmeOperator)
	siblingGRN, _ := provisioned(t, url, acmeOperator)
	recipientGRN, recipient := provisioned(t, url, globexOperator)
	bystanderGRN, _ := provisioned(t, url, initechOperator)
	consents := message.NewPostgresStore(p)
	queue := url + "/agents/" + recipientGRN.String() + "/claims"

	// No relation anywhere: the post comes back to the sender.
	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))
	var answer machinegen.Message
	require.NoError(t, json.Unmarshal(body, &answer))
	assert.Equal(t, senderGRN.String(), answer.Recipient, "no consent: the refusal returns to the sender")

	// One half alone carries nothing.
	_, err = consents.Grant(ctx, orgRootGRN("acme"), orgRootGRN("globex"))
	require.NoError(t, err)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &answer))
	assert.Equal(t, senderGRN.String(), answer.Recipient, "one half does not carry a message across")

	// Both halves standing, the same call crosses and is claimable.
	_, err = consents.Grant(ctx, orgRootGRN("globex"), orgRootGRN("acme"))
	require.NoError(t, err)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &answer))
	assert.Equal(t, recipientGRN.String(), answer.Recipient, "the standing relation stored the message itself")

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, answer.Id, claimed.Message.Id)
	assert.Equal(t, senderGRN.String(), claimed.Message.Sender)
	assert.Equal(t, "hello", claimed.Message.Body)
	status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))

	// A relation between two other organizations admits nothing for
	// this pair.
	_, err = consents.Grant(ctx, orgRootGRN("globex"), orgRootGRN("initech"))
	require.NoError(t, err)
	_, err = consents.Grant(ctx, orgRootGRN("initech"), orgRootGRN("globex"))
	require.NoError(t, err)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(bystanderGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &answer))
	assert.Equal(t, senderGRN.String(), answer.Recipient, "globex consenting elsewhere joins acme to nobody")

	// Withdrawal closes the boundary at once: the next post comes back
	// to the sender, and the half acme left standing is what a re-grant
	// joins.
	require.NoError(t, consents.Withdraw(ctx, orgRootGRN("globex"), orgRootGRN("acme")))
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-5"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &answer))
	assert.Equal(t, senderGRN.String(), answer.Recipient, "withdrawn, the relation refuses again")
	_, err = consents.Grant(ctx, orgRootGRN("globex"), orgRootGRN("acme"))
	require.NoError(t, err)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-6"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &answer))
	assert.Equal(t, recipientGRN.String(), answer.Recipient, "the re-grant crosses again")

	// The same-organization post is unaffected by all of it.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(siblingGRN, "key-7"))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &answer))
	assert.Equal(t, siblingGRN.String(), answer.Recipient)

	// The recipient's queue holds exactly the two messages that
	// crossed: every accepted one and no refused one.
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, "key-6", claimed.Message.IdempotencyKey)
	status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a refused message reached the queue")
}
