package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/org"
)

// mintedIn records a token for the fixture's agent, minted by its
// controller through the store, and answers it.
func (f fence) mintedIn(t *testing.T) *agent.AccessToken {
	t.Helper()
	var b [32]byte
	_, err := rand.Read(b[:])
	require.NoError(t, err)
	now := time.Now()
	token := &agent.AccessToken{Value: agent.AccessTokenPrefix + hex.EncodeToString(b[:]), Agent: f.agent,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	require.NoError(t, f.agents.MintAccessToken(context.Background(), f.controller, token))
	return token
}

// presented is the credential the execution fence is handed for a
// token: its serial and dates, as the listener answers them.
func presented(token *agent.AccessToken) agent.Credential {
	return agent.Credential{Serial: token.Serial(), NotBefore: token.IssuedAt, NotAfter: token.ExpiresAt}
}

// TestAgentToken_TheFenceRefusesAReassignedTokenOnItsOwn calls the
// execution fence (ADR-0080) directly, as a protected write does inside
// its own transaction, with no listener read before it — the case of a
// reassignment that commits between the listener's resolve and the
// write. The token is admitted before the reassignment and fenced
// after it. A certificate of the same legacy agent is admitted on both
// sides, so what fences the token is the fence's token branch and not
// the reassignment fencing everything.
func TestAgentToken_TheFenceRefusesAReassignedTokenOnItsOwn(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFence(t)
	token := f.mintedIn(t)

	require.Equal(t, agent.AdmissionAdmitted, f.admit(t, nil, presented(token)))
	require.Equal(t, agent.AdmissionAdmitted, f.admit(t, nil, valid("1")))

	_, err := f.agents.Assign(ctx, f.agent, scopedGRN("acme", agent.ServiceTypeOperator, "elsewhere"))
	require.NoError(t, err)

	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, nil, presented(token)),
		"the fence refuses a token minted at the earlier assignment")
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, nil, valid("1")),
		"control: the legacy agent's certificate is still admitted")
}

// TestAgentToken_ARecoveryFencesTheTokensOfTheLineageItReplaces is
// ADR-0078's recovery reaching a token: one minted in the lineage a
// recovery opened is admitted, and fenced once a second recovery
// replaces that lineage — by the fence and by the listener's read
// alike. A token minted in the new lineage is the control.
func TestAgentToken_ARecoveryFencesTheTokensOfTheLineageItReplaces(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	a1, _ := f.activate(t, "r1", generation(1), nil, valid("1"))
	first := f.recovery("rec-1", "ref-1", 1, valid("2"))
	first.Epoch = f.epoch(t)
	_, err := f.exec.Recover(ctx, first)
	require.NoError(t, err)

	token := f.mintedIn(t)
	require.NotNil(t, token.Lineage)
	assert.Equal(t, first.Lineage, *token.Lineage, "the token is minted in the lineage the recovery opened")
	a2, t2 := f.activate(t, "r2", generation(1), &a1.ID, valid("2"))
	require.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t2, presented(token)))
	_, err = f.agents.ResolveAccessToken(ctx, token.Hash())
	require.NoError(t, err)

	second := f.recovery("rec-2", "ref-2", 2, valid("3"))
	second.Epoch = f.epoch(t)
	_, err = f.exec.Recover(ctx, second)
	require.NoError(t, err)
	_, t3 := f.activate(t, "r3", generation(3), &a2.ID, valid("3"))

	assert.Equal(t, agent.AdmissionCredentialFenced, f.admit(t, &t3, presented(token)),
		"the fence refuses a token of the replaced lineage")
	_, err = f.agents.ResolveAccessToken(ctx, token.Hash())
	assert.ErrorIs(t, err, agent.ErrAccessTokenInvalid, "and so does the listener's read")

	fresh := f.mintedIn(t)
	assert.Equal(t, agent.AdmissionAdmitted, f.admit(t, &t3, presented(fresh)),
		"control: a token minted in the new lineage is admitted")
}

// TestAgentToken_DeletingTheAgentTakesItsTokens holds the cascade: an
// agent goes with its organization, and its tokens go with it, so the
// listener's read finds none. The read before the deletion is the
// control.
func TestAgentToken_DeletingTheAgentTakesItsTokens(t *testing.T) {
	ctx := context.Background()
	f := newLegacyFence(t)
	token := f.mintedIn(t)
	_, err := f.agents.ResolveAccessToken(ctx, token.Hash())
	require.NoError(t, err)

	require.NoError(t, org.NewPostgresStore(f.p).DeleteOrg(ctx, orgRootGRN("acme")))

	_, err = f.agents.ResolveAccessToken(ctx, token.Hash())
	assert.ErrorIs(t, err, agent.ErrAccessTokenInvalid)
	assert.Equal(t, 0, countRows(t, f.p, `SELECT count(*) FROM agent_tokens WHERE agent_grn = $1`, f.agent.String()))
}
