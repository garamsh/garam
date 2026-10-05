package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// inputs is what an activation is sought on and what re-arms one a
// definite refusal paused: the binding the runtime reports, the
// placement token's fingerprint and the agent certificate's serial
// (ADR-0084). Only the first two decide the request identifier.
type inputs struct {
	binding   Binding
	placement string
	leaf      string
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
	// delay is how long until the next look: the status interval, or
	// backoff while an undecided activation is retried.
	delay   time.Duration
	backoff time.Duration
}

// supervise keeps the execution matched to the runtime beside it: for
// a fenced agent it activates the generation the runtime reports and
// reports that runtime's status, and for a legacy one it reopens the
// gate when the certificate a refusal was met under is renewed.
func (s *service) supervise(ctx context.Context) {
	var sup supervision
	for ctx.Err() == nil {
		sup.delay = s.cfg.StatusInterval
		if s.ends.Execution.fenced {
			s.superviseFenced(ctx, &sup)
		} else {
			s.superviseLegacy(ctx, &sup)
		}
		s.wait(ctx, sup.delay)
	}
}

// superviseLegacy reopens a refused legacy execution once the agent's
// certificate has changed, and never on a timer.
func (s *service) superviseLegacy(ctx context.Context, sup *supervision) {
	leaf, err := s.ends.Credentials.LeafSerial()
	if err != nil {
		slog.WarnContext(ctx, "agent certificate not read", slog.String("error", err.Error()))
		return
	}
	gate, _ := s.ends.Execution.state()
	if gate == gateRefused && leaf != sup.at.leaf {
		slog.InfoContext(ctx, "protected work resumed on a renewed certificate")
		s.ends.Execution.open()
	}
	sup.at.leaf = leaf
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
			// cannot be activated for, and what was held is not its.
			s.ends.Execution.close(gateClosed)
		}
		slog.WarnContext(ctx, "runtime status not read", slog.String("error", err.Error()))
		return
	}
	token, err := s.ends.Credentials.PlacementToken()
	if err != nil {
		slog.WarnContext(ctx, "placement token not read", slog.String("error", err.Error()))
		return
	}
	leaf, err := s.ends.Credentials.LeafSerial()
	if err != nil {
		slog.WarnContext(ctx, "agent certificate not read", slog.String("error", err.Error()))
		return
	}
	fingerprint := sha256.Sum256([]byte(token))
	now := inputs{binding: BindingOf(status), placement: hex.EncodeToString(fingerprint[:]), leaf: leaf}

	gate, held := s.ends.Execution.state()
	switch {
	case gate == gateOpen && held.Binding == now.binding && sup.at.placement == now.placement:
		s.report(ctx, sup, held, status)
		return
	case gate == gateRefused && sup.at == now:
		// Paused on a definite refusal: nothing is resent until an
		// input changes.
		return
	}
	s.ends.Execution.close(gateClosed)
	s.activate(ctx, sup, now, token, status)
}

// activate asks for the activation of what the runtime reports. An
// undecided answer is retried with backoff under the same request; a
// definite one pauses on the inputs it was asked under.
func (s *service) activate(ctx context.Context, sup *supervision, now inputs, token string, status *RuntimeStatus) {
	requestID := RequestID(now.binding, token)
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
	sup.reported, sup.declined = nil, nil
	slog.InfoContext(ctx, "activation held",
		slog.String("activation", activation.ID),
		slog.Int("token_version", activation.TokenVersion),
		slog.String("generation", now.binding.Generation))
	s.report(ctx, sup, activation, status)
}

// report carries the runtime's status to control when it differs from
// what control last accepted or refused. An undecided answer is retried
// on the next look with whatever is observed then.
func (s *service) report(ctx context.Context, sup *supervision, held *Activation, status *RuntimeStatus) {
	if sameStatus(sup.reported, status) || sameStatus(sup.declined, status) {
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
