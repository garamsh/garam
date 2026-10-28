package channel

import (
	"context"
	"crypto/rand"
	"slices"
	"strings"
	"time"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// ErrDirectIsFixed is changing who a direct message is between.
var ErrDirectIsFixed = errorx.New(errorx.KindFailedPrecondition, "a direct message is between the people it started with")

const (
	idAlphabet = "abcdefghijklmnopqrstuvwxyz234567"
	idLength   = 12
)

// newID is 12 characters of 32, which a GRN identifier accepts.
func newID() string {
	b := make([]byte, idLength)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return string(b)
}

// Create adds a channel. Its creator is on its list where it keeps
// one. A direct message is between its creator and Members, at least
// one other.
func (s *service) Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Channel, error) {
	c := Channel{ID: newID(), Org: org, Kind: in.Kind, Name: strings.TrimSpace(in.Name), CreatedBy: caller.GRN, CreatedAt: time.Now()}
	switch c.Kind {
	case KindPublic, KindPrivate:
		if c.Name == "" || len(c.Name) > maxName {
			return Channel{}, ErrInvalidChannel
		}
	case KindDirect:
		if c.Name != "" {
			return Channel{}, ErrInvalidChannel
		}
	default:
		return Channel{}, ErrInvalidChannel
	}
	if c.Lists() {
		c.Members = []grn.GRN{caller.GRN}
		for _, m := range in.Members {
			if err := m.Validate(); err != nil {
				return Channel{}, errorx.Wrap(err, "member", errorx.KindInvalidArgument)
			}
			if !slices.Contains(c.Members, m) {
				c.Members = append(c.Members, m)
			}
		}
		if c.Kind == KindDirect && len(c.Members) < 2 {
			return Channel{}, ErrInvalidChannel
		}
	}
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: ActionPost}); err != nil {
		return Channel{}, err
	}
	if err := s.repo.Create(ctx, c); err != nil {
		return Channel{}, err
	}
	return c, nil
}

// List answers the channels the caller may read.
func (s *service) List(ctx context.Context, caller platform.Caller, org string) ([]Channel, error) {
	return s.readable(ctx, caller, org)
}

// Get answers one channel the caller may read.
func (s *service) Get(ctx context.Context, caller platform.Caller, org, id string) (Channel, error) {
	return s.channel(ctx, caller, org, id, ActionRead)
}

// AddMember puts another member on a private channel's list. Only a
// participant who may post there adds one; a direct message's list is
// fixed.
func (s *service) AddMember(ctx context.Context, caller platform.Caller, org, id string, member grn.GRN) error {
	if err := member.Validate(); err != nil {
		return errorx.Wrap(err, "member", errorx.KindInvalidArgument)
	}
	c, err := s.channel(ctx, caller, org, id, ActionPost)
	if err != nil {
		return err
	}
	switch c.Kind {
	case KindPublic:
		return ErrPublicHasNoList
	case KindDirect:
		return ErrDirectIsFixed
	}
	return s.repo.AddMember(ctx, org, id, member)
}

// Leave takes the caller off a channel's list. Leaving needs no allow
// beyond being on it.
func (s *service) Leave(ctx context.Context, caller platform.Caller, org, id string) error {
	c, err := s.repo.Get(ctx, org, id)
	if err != nil {
		return err
	}
	if !c.Lists() {
		return ErrPublicHasNoList
	}
	if !c.HasMember(caller.GRN) {
		return ErrNotParticipant
	}
	return s.repo.RemoveMember(ctx, org, id, caller.GRN)
}
