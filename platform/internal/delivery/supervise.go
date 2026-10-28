package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/garamsh/garam/lib/errorx"
)

// inputs is what an activation is sought on and what re-arms one a
// definite refusal paused: the binding the runtime reports, the
// placement token's fingerprint and the fingerprint of what the
// adapter's calls present — its token files' hashes (ADR-0084,
// ADR-0100). Only the first two decide
// the request identifier, and a re-armed activation is asked again,
// never assumed.
type inputs struct {
	binding    Binding
	placement  string
	credential string
}

// supervision is the supervisor's memory of what it last acted on.
type supervision struct {
	// at is the inputs the held activation was obtained on, or that a
	// refusal paused on.
	at inputs
	// requestID and activationID are the last activation obtained, so a
	// replay answering another activation is seen as a wrong binding.
	requestID    string
	activationID string
	// reported and declined are the last observation control accepted
	// and the last one it refused; neither is sent again unchanged.
	reported *RuntimeStatus
	declined *RuntimeStatus
	// superseded is a generation control said is not current, which is
	// reported no more.
	superseded string
	// delay is how long until the next look: the status interval, or
	// backoff while an undecided activation is retried.
	delay   time.Duration
	backoff time.Duration
}

// supervise keeps the execution matched to the runtime beside it: it
// activates the generation the runtime reports and reports that
// runtime's status.
func (s *service) supervise(ctx context.Context) {
	var sup supervision
	for ctx.Err() == nil {
		sup.delay = s.cfg.StatusInterval
		s.superviseFenced(ctx, &sup)
		s.wait(ctx, sup.delay)
	}
}

// superviseFenced reads the runtime and the credentials once and acts
// on what changed.
func (s *service) superviseFenced(ctx context.Context, sup *supervision) {
	statusCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	status, err := s.ends.Gateway.Status(statusCtx)
	cancel()
	if err != nil {
		if !errorx.IsKind(err, errorx.KindUnavailable) {
			// A runtime of another agent, contract or no generation
			// cannot be activated for, and what was held is not its. A
			// paused gate and the inputs it paused on are left alone.
			s.ends.Execution.withdraw()
		}
		slog.WarnContext(ctx, "runtime status not read", slog.String("error", err.Error()))
		return
	}
	token, err := s.ends.Credentials.PlacementToken()
	if err != nil {
		slog.WarnContext(ctx, "placement token not read", slog.String("error", err.Error()))
		return
	}
	credential, err := s.ends.Credentials.Fingerprint()
	if err != nil {
		slog.WarnContext(ctx, "agent credential not read", slog.String("error", err.Error()))
		return
	}
	fingerprint := sha256.Sum256([]byte(token))
	now := inputs{binding: BindingOf(status), placement: hex.EncodeToString(fingerprint[:]), credential: credential}

	gate, held := s.ends.Execution.state()
	switch {
	case gate == gateOpen && held.Binding == now.binding && sup.at.placement == now.placement:
		s.report(ctx, sup, held, status)
		return
	case gate == gateRefused && sup.at == now:
		// Paused on a definite refusal: nothing is resent until an
		// input changes.
		return
	case status.State != RuntimeServing:
		// A runtime that is not serving is never activated: one already
		// told to stop is not the generation to fence the next one by
		// (ADR-0085). What was held is not its, and a pause stays.
		s.ends.Execution.withdraw()
		slog.InfoContext(ctx, "runtime not serving; not activated",
			slog.String("generation", now.binding.Generation), slog.String("state", string(status.State)))
		return
	}
	s.ends.Execution.close(gateClosed)
	s.activate(ctx, sup, now, token, status)
}

// activate asks for the activation of what the runtime reports. An
// undecided answer is retried with backoff under the same request; a
// definite one pauses on the inputs it was asked under.
func (s *service) activate(ctx context.Context, sup *supervision, now inputs, token string, status *RuntimeStatus) {
	requestID := RequestID(s.ends.Authority.Contract(), now.binding, token)
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	activation, err := s.ends.Authority.Activate(callCtx, requestID, now.binding, token)
	cancel()
	if err == nil && requestID == sup.requestID && activation.ID != sup.activationID {
		err = errorx.New(errorx.KindFailedPrecondition, "delivery: control answered a replay with another activation")
	}
	if errorx.IsKind(err, errorx.KindUnavailable) {
		sup.backoff = s.backoff(sup.backoff)
		sup.delay = sup.backoff
		slog.WarnContext(ctx, "activation undecided",
			slog.String("request", requestID),
			slog.String("generation", now.binding.Generation),
			slog.String("error", err.Error()))
		return
	}
	sup.backoff = 0
	if err != nil {
		sup.at = now
		s.ends.Execution.close(gateRefused)
		slog.ErrorContext(ctx, "activation refused; protected work stopped",
			slog.String("request", requestID),
			slog.String("generation", now.binding.Generation),
			slog.Bool("unsupported_peer", errorx.IsKind(err, errorx.KindUnimplemented)),
			slog.String("error", err.Error()))
		return
	}
	if !s.ends.Execution.hold(activation) {
		slog.WarnContext(ctx, "activation answer discarded: not newer than the token held",
			slog.String("activation", activation.ID),
			slog.Int("token_version", activation.TokenVersion))
		return
	}
	sup.at, sup.requestID, sup.activationID = now, requestID, activation.ID
	sup.reported, sup.declined, sup.superseded = nil, nil, ""
	slog.InfoContext(ctx, "activation held",
		slog.String("activation", activation.ID),
		slog.Int("token_version", activation.TokenVersion),
		slog.String("generation", now.binding.Generation))
	s.report(ctx, sup, activation, status)
}

// report carries the runtime's status to control when it differs from
// what control last accepted or refused. An undecided answer is retried
// on the next look with whatever is observed then; a generation control
// says is not current is reported no more until another activation.
func (s *service) report(ctx context.Context, sup *supervision, held *Activation, status *RuntimeStatus) {
	if sameStatus(sup.reported, status) || sameStatus(sup.declined, status) ||
		status.Generation == sup.superseded {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	err := s.ends.Authority.Report(callCtx, held.ID, status, time.Now())
	cancel()
	switch {
	case err == nil:
		sup.reported = status
	case errorx.IsKind(err, errorx.KindUnavailable):
		slog.WarnContext(ctx, "runtime status not reported", slog.String("error", err.Error()))
	case errorx.IsKind(err, errorx.KindAborted):
		sup.superseded = status.Generation
		slog.WarnContext(ctx, "runtime status no longer reported for this generation",
			slog.String("generation", status.Generation),
			slog.String("error", err.Error()))
	default:
		sup.declined = status
		slog.WarnContext(ctx, "runtime status refused",
			slog.String("generation", status.Generation),
			slog.String("error", err.Error()))
	}
}

// sameStatus reports whether two observations say the same thing.
func sameStatus(a, b *RuntimeStatus) bool {
	return a != nil && b != nil && *a == *b
}
