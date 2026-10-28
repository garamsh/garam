package channel

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/fanout"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/grn"
)

// The platform's actions on channels (ADR-0128).
const (
	ActionRead = "cowork:channel-read"
	ActionPost = "cowork:channel-post"
)

// Service is messaging as a caller acts on it. Every method asks the
// platform, and for a channel that keeps a list, the list after it
// (ADR-0128): reading needs cowork:channel-read, posting and adding a
// participant cowork:channel-post, on the channel and, for listing,
// searching and creating, on the organization.
type Service interface {
	Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Channel, error)
	List(ctx context.Context, caller platform.Caller, org string) ([]Channel, error)
	Get(ctx context.Context, caller platform.Caller, org, id string) (Channel, error)
	AddMember(ctx context.Context, caller platform.Caller, org, id string, member grn.GRN) error
	Leave(ctx context.Context, caller platform.Caller, org, id string) error

	Post(ctx context.Context, caller platform.Caller, org, channel string, in PostInput) (Message, error)
	// Messages answers the channel's messages before before, as
	// Repository.Messages does.
	Messages(ctx context.Context, caller platform.Caller, org, channel, before string) ([]Message, error)
	// Thread answers the thread a message starts or is in.
	Thread(ctx context.Context, caller platform.Caller, org, message string) ([]Message, error)
	// Search answers the messages matching query in the channels the
	// caller may read.
	Search(ctx context.Context, caller platform.Caller, org, query string) ([]Message, error)
	// Linking answers the messages linking a page the caller may read,
	// in the channels the caller may read.
	Linking(ctx context.Context, caller platform.Caller, org, page string) ([]Message, error)
	// Listen delivers every message posted in a channel the caller may
	// read, as Live encodes it, until leave is called.
	Listen(ctx context.Context, caller platform.Caller, org, channel string, peer fanout.Peer) (leave func(), err error)
}

type service struct {
	repo     Repository
	platform platform.Platform
	hub      fanout.Hub
}

// NewService answers messaging over repo, deciding every act through
// p and delivering live through hub.
func NewService(repo Repository, p platform.Platform, hub fanout.Hub) Service {
	return &service{repo: repo, platform: p, hub: hub}
}

// room is a channel's room on the hub.
func room(org, id string) string { return "channel/" + org + "/" + id }

// access answers nil when the caller may take action on c: the
// platform first, so a caller it refuses learns nothing of the list,
// then the list, where the channel keeps one.
func (s *service) access(ctx context.Context, caller platform.Caller, c Channel, action string) error {
	if err := platform.Require(ctx, s.platform, caller, c.Org, platform.OnChannel(action, c.ID)); err != nil {
		return err
	}
	if c.Lists() && !c.HasMember(caller.GRN) {
		return ErrNotParticipant
	}
	return nil
}

// channel reads one channel the caller may take action on.
func (s *service) channel(ctx context.Context, caller platform.Caller, org, id, action string) (Channel, error) {
	c, err := s.repo.Get(ctx, org, id)
	if err != nil {
		return Channel{}, err
	}
	return c, s.access(ctx, caller, c, action)
}

// readable answers the channels of the organization the caller may
// read: those the repository lists for it, less those the platform
// denies, asked at most 50 at a time.
func (s *service) readable(ctx context.Context, caller platform.Caller, org string) ([]Channel, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: ActionRead}); err != nil {
		return nil, err
	}
	listed, err := s.repo.List(ctx, org, caller.GRN)
	if err != nil {
		return nil, err
	}
	var out []Channel
	for start := 0; start < len(listed); start += 50 {
		batch := listed[start:min(start+50, len(listed))]
		checks := make([]platform.Check, len(batch))
		for i, c := range batch {
			checks[i] = platform.OnChannel(ActionRead, c.ID)
		}
		allowed, err := s.platform.Allowed(ctx, caller, org, checks)
		if err != nil {
			return nil, err
		}
		for i, c := range batch {
			if allowed[i] {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func ids(cs []Channel) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}
