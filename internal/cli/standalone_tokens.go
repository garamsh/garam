package cli

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/keyfile"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/workload"
)

// The refresh window ADR-0100 sets: a holder replaces a token at a
// point drawn from two thirds to three quarters of its life, so a fleet
// does not refresh at once and a quarter of the life is left for the
// new one to reach whoever reads it.
const (
	refreshFrom = 2.0 / 3
	refreshTo   = 3.0 / 4
)

// mintedAgentToken is an agent token as api/machine.yaml spells one.
var mintedAgentToken = regexp.MustCompile(`^garam_at_[0-9a-f]{64}$`)

// heldToken is a token held in memory and when it is replaced.
type heldToken struct {
	value     string
	expiresAt time.Time
	refreshAt time.Time
}

// held is a token answered at now, to be replaced at a point drawn
// from the refresh window of what is left of its life.
func held(value string, expiresAt, now time.Time) heldToken {
	life := expiresAt.Sub(now)
	at := refreshFrom + rand.Float64()*(refreshTo-refreshFrom)
	return heldToken{value: value, expiresAt: expiresAt, refreshAt: now.Add(time.Duration(float64(life) * at))}
}

// fresh reports a token not yet due for replacement, and live one not
// yet expired.
func (h heldToken) fresh(now time.Time) bool { return h.value != "" && now.Before(h.refreshAt) }
func (h heldToken) live(now time.Time) bool  { return h.value != "" && now.Before(h.expiresAt) }

// supervisorTokens is a v2 supervisor's tokens (ADR-0100): its own,
// exchanged from its credential, and its agent's for garam, minted with
// its own. Both live in memory alone. The credential is read from its
// owner-only file for each exchange and never otherwise; no token or
// credential reaches the environment, a log line or a URL. It is the
// workload's AgentTokens and its Activator at once, since the
// supervisor forwards the agent token it minted.
type supervisorTokens struct {
	client     *http.Client
	base       string
	agent      grn.GRN
	credential string

	mu         sync.Mutex
	own        heldToken
	agentToken heldToken
}

// newSupervisorTokens reaches garam at base, verified by the server
// root the enrollment installed in dir beside the credential.
func newSupervisorTokens(dir, base string, agentGRN grn.GRN) (*supervisorTokens, error) {
	rootPEM, err := os.ReadFile(filepath.Join(dir, serverRootFile))
	if err != nil {
		return nil, errorx.Wrap(err, "server root", errorx.KindFailedPrecondition)
	}
	client, err := standaloneClient(rootPEM)
	if err != nil {
		return nil, err
	}
	credential := filepath.Join(dir, credentialFile)
	if _, err := keyfile.Read(credential); err != nil {
		return nil, errorx.Wrap(err, "supervisor credential", errorx.KindFailedPrecondition)
	}
	return &supervisorTokens{client: client, base: base, agent: agentGRN, credential: credential}, nil
}

// Current answers the agent's garam token, minting the next once the
// one held is inside its refresh window. A mint garam did not answer,
// while the one held is still live, answers that one, and the next call
// tries again. A mint garam refused drops the one held: what refused
// the supervisor is a revocation or a replacement, which refuses that
// token too, so it is not handed out until it expires.
func (s *supervisorTokens) Current(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.agentToken.fresh(now) {
		return s.agentToken.value, nil
	}
	minted, err := s.mint(ctx)
	if err != nil {
		if !errorx.IsKind(err, errorx.KindUnavailable) {
			s.agentToken = heldToken{}
			return "", err
		}
		if s.agentToken.live(now) {
			slog.Warn("agent token not refreshed; the held one is still live", slog.String("error", err.Error()))
			return s.agentToken.value, nil
		}
		return "", err
	}
	s.agentToken = minted
	slog.Info("agent token minted", slog.Time("expires_at", minted.expiresAt))
	return minted.value, nil
}

// Activate calls activateAgent under execution-fence.v2 with the
// supervisor's own token, forwarding the agent token the request
// carries.
func (s *supervisorTokens) Activate(ctx context.Context, req workload.ActivationRequest) (*workload.Activation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body := machinegen.ActivationRequest{
		RequestId: req.RequestID, Epoch: req.Epoch, Generation: req.Generation, Token: req.Token,
	}
	if req.Replaces != "" {
		body.ReplacesActivationId = &req.Replaces
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, errorx.Wrap(err, "activation", errorx.KindInternal)
	}
	var answer machinegen.Activation
	status, err := s.call(ctx, "/agents/"+s.agent.String()+"/activations", agent.TokenExecutionContractVersion, raw, &answer)
	if err != nil {
		return nil, err
	}
	return &workload.Activation{
		ID: answer.ActivationId, Token: answer.Token, TokenVersion: answer.TokenVersion,
		GRN: answer.Grn, Epoch: answer.Epoch, Generation: answer.Generation,
		Replayed: status == http.StatusOK,
	}, nil
}

// mint answers a new agent token for garam. The caller holds mu.
func (s *supervisorTokens) mint(ctx context.Context) (heldToken, error) {
	var answer machinegen.AgentToken
	if _, err := s.call(ctx, "/agents/"+s.agent.String()+"/tokens", operator.TokenContractVersion,
		[]byte("{}"), &answer); err != nil {
		return heldToken{}, err
	}
	if !mintedAgentToken.MatchString(answer.Token) || answer.Grn != s.agent.String() {
		return heldToken{}, errorx.New(errorx.KindInternal, "garam answered a mint without this agent's token")
	}
	return held(answer.Token, answer.ExpiresAt, time.Now()), nil
}

// call posts under contract with the supervisor's own token, exchanging
// it first where the one held is due. garam refusing it drops it, so
// the next call exchanges afresh. The caller holds mu.
func (s *supervisorTokens) call(ctx context.Context, path, contract string, body []byte, out any) (int, error) {
	own, err := s.ownToken(ctx)
	if err != nil {
		return 0, err
	}
	status, err := callStandalone(ctx, s.client, s.base+path, contract, machineTokenScheme+" "+own, body, out)
	if errorx.IsKind(err, errorx.KindUnauthenticated) {
		s.own = heldToken{}
	}
	return status, err
}

// machineTokenScheme is what a token is presented under (ADR-0100).
const machineTokenScheme = "Garam-Token"

// ownToken answers the supervisor's token, exchanging the credential
// for the next once the one held is inside its refresh window. The
// caller holds mu.
func (s *supervisorTokens) ownToken(ctx context.Context) (string, error) {
	now := time.Now()
	if s.own.fresh(now) {
		return s.own.value, nil
	}
	credential, err := keyfile.Read(s.credential)
	if err != nil {
		return "", errorx.Wrap(err, "supervisor credential", errorx.KindFailedPrecondition)
	}
	var answer machinegen.MachineToken
	_, err = callStandalone(ctx, s.client, s.base+"/tokens", operator.TokenContractVersion,
		"Garam-Credential "+strings.TrimSpace(string(credential)), []byte("{}"), &answer)
	if err != nil {
		if s.own.live(now) {
			slog.Warn("supervisor token not refreshed; the held one is still live", slog.String("error", err.Error()))
			return s.own.value, nil
		}
		return "", errorx.Wrap(err, "exchange the supervisor credential")
	}
	s.own = held(answer.Token, answer.ExpiresAt, now)
	return s.own.value, nil
}
