package machineapi

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// WithSendPolicy makes the policy service the decision of every send,
// consent included, and reads the sender's own organization's agents
// through held to tell a recipient that names no agent from one the
// service can decide (ADR-0168). Without it a send is answered as
// unavailable rather than admitted: nothing else decides one.
func (s *Server) WithSendPolicy(decisions policydecision.Service, held AgentReader) *Server {
	s.decisions = decisions
	s.held = held
	return s
}

// errSendDenied is the denial of one send inside the sender's
// organization, which is a refusal garam records rather than a failure
// to decide.
var errSendDenied = errorx.New(errorx.KindPermissionDenied, "sender may not send to this recipient")

// errNoSendPolicy is what a send answers when no policy service is
// configured.
var errNoSendPolicy = errorx.New(errorx.KindUnavailable, "no policy service is configured to decide a send")

// crossingRefusal is what a post the policy service does not carry
// across its boundary stores instead. garam cannot route the message,
// so the answer is not a status: the refusal returns to the sender as
// an ordinary inbound message (ADR-0013), and the post is answered once
// that is stored, as for any other. It is sent under the intended
// recipient's identity because garam holds none of its own (ADR-0024);
// the body names neither consent nor absence, so it tells the sender
// nothing about the other organization (ADR-0168), and the key derives
// from the sender's so a retried post is answered with the refusal
// already stored rather than piling up one per retry. garam is its
// author, so its signature names garam as the sender it vouches for
// (ADR-0101).
func crossingRefusal(sender, recipient grn.GRN, key string) (from, to grn.GRN, refusalKey, body string, author message.Author) {
	body = "garam could not deliver your message to " + recipient.String() +
		": a message to this recipient is not permitted"
	return recipient, sender, "consent-refused:" + sender.String() + ":" + key, body, message.AuthorGaram
}

// authorizeSend reports whether sender's post to recipient is admitted
// to the queue. The question the policy service could hold is chosen
// first (ADR-0168):
//
//   - inside the sender's organization, a recipient that names no agent
//     garam holds is admitted unasked and stored as a routing failure
//     (ADR-0013);
//   - across the boundary, a recipient that is not an agent type is
//     refused unasked;
//   - every other recipient is the service's decision.
//
// A refusal across the boundary is not an error: admitted is false, and
// the caller answers the post with the refusal message. A denial inside
// the organization is the error, recorded as a refused send before it
// is answered (ADR-0106). A service that cannot answer is the error
// too, and nothing is stored.
func (s *Server) authorizeSend(ctx context.Context, sender, recipient grn.GRN, fence *message.Fence) (bool, error) {
	if err := sender.Validate(); err != nil {
		return false, err
	}
	if err := recipient.Validate(); err != nil {
		return false, err
	}
	inside := sender.Organization == recipient.Organization
	if inside {
		held, err := s.holdsAgent(ctx, recipient)
		if err != nil {
			return false, err
		}
		if !held {
			return true, nil
		}
	} else if recipient.ServiceType != agent.ServiceType {
		return false, nil
	}
	allowed, err := s.decide(ctx, sender, recipient)
	if err != nil {
		return false, err
	}
	if allowed {
		return true, nil
	}
	if !inside {
		return false, nil
	}
	if refused := s.messages.Refuse(ctx, sender, recipient, fence); refused != nil {
		return false, refused
	}
	return false, errSendDenied
}

// holdsAgent reports whether recipient names an agent garam holds. A
// recipient of another type names none, whatever its id.
func (s *Server) holdsAgent(ctx context.Context, recipient grn.GRN) (bool, error) {
	if recipient.ServiceType != agent.ServiceType {
		return false, nil
	}
	if s.held == nil {
		return false, errNoSendPolicy
	}
	_, err := s.held.Get(ctx, recipient)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return false, nil
	}
	return err == nil, err
}

// decide asks the policy service whether sender may send to recipient.
func (s *Server) decide(ctx context.Context, sender, recipient grn.GRN) (bool, error) {
	if s.decisions == nil {
		return false, errNoSendPolicy
	}
	d, err := s.decisions.Evaluate(ctx, policydecision.Question{
		Subject:  sender,
		Action:   message.ActionSend,
		Resource: recipient,
	})
	if err != nil {
		// Whatever the service's status, a send it did not decide is
		// unavailable, never a permission denial the sender could read
		// as the policy's answer.
		return false, errorx.Wrap(err, "policy service", errorx.KindUnavailable)
	}
	return d.Allowed, nil
}
