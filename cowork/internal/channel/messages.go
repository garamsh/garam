package channel

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
)

// ErrPageNotReadable is linking a page the caller may not read: the
// link would show its title to the channel.
var ErrPageNotReadable = errorx.New(errorx.KindPermissionDenied, "a message links only pages its author may read")

// ErrArtifactNotReadable is embedding an artifact the caller may not
// read, which would show it to the channel.
var ErrArtifactNotReadable = errorx.New(errorx.KindPermissionDenied, "a message embeds only artifacts its author may read")

// Post adds a message to a channel the caller may post in, and
// delivers it to the channel's listeners. Every page it links is one
// the caller may read.
func (s *service) Post(ctx context.Context, caller platform.Caller, org, id string, in PostInput) (Message, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" || len(body) > maxBody || len(in.Mentions) > maxMentions || len(in.Pages) > maxPages || len(in.Artifacts) > maxArtifacts {
		return Message{}, ErrInvalidMessage
	}
	for _, m := range in.Mentions {
		if err := m.Validate(); err != nil {
			return Message{}, errorx.Wrap(err, "mention", errorx.KindInvalidArgument)
		}
	}
	c, err := s.channel(ctx, caller, org, id, ActionPost)
	if err != nil {
		return Message{}, err
	}
	pages := slices.Compact(slices.Sorted(slices.Values(in.Pages)))
	if len(pages) > 0 {
		checks := make([]platform.Check, len(pages))
		for i, p := range pages {
			checks[i] = platform.OnPage(platform.ActionPageRead, p)
		}
		if err := platform.Require(ctx, s.platform, caller, org, checks...); err != nil {
			if errorx.IsKind(err, errorx.KindPermissionDenied) {
				return Message{}, ErrPageNotReadable
			}
			return Message{}, err
		}
	}
	artifacts := slices.Compact(slices.Sorted(slices.Values(in.Artifacts)))
	if len(artifacts) > 0 {
		checks := make([]platform.Check, len(artifacts))
		for i, a := range artifacts {
			checks[i] = platform.Check{Action: platform.ActionArtifactRead, Kind: platform.KindArtifact, ID: a}
		}
		if err := platform.Require(ctx, s.platform, caller, org, checks...); err != nil {
			if errorx.IsKind(err, errorx.KindPermissionDenied) {
				return Message{}, ErrArtifactNotReadable
			}
			return Message{}, err
		}
	}
	m, err := s.repo.Post(ctx, Message{
		ID:        newID(),
		Org:       org,
		Channel:   c.ID,
		Thread:    in.Thread,
		Author:    caller.GRN,
		Body:      body,
		Mentions:  unique(in.Mentions),
		Pages:     pages,
		Artifacts: artifacts,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return Message{}, err
	}
	s.hub.Publish(room(org, c.ID), nil, Live(m))
	return m, nil
}

// Messages answers a channel's messages the caller may read.
func (s *service) Messages(ctx context.Context, caller platform.Caller, org, id, before string) ([]Message, error) {
	if _, err := s.channel(ctx, caller, org, id, ActionRead); err != nil {
		return nil, err
	}
	return s.repo.Messages(ctx, org, id, before, maxListed)
}

// Thread answers the thread a message is in, in a channel the caller
// may read.
func (s *service) Thread(ctx context.Context, caller platform.Caller, org, id string) ([]Message, error) {
	m, err := s.repo.GetMessage(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.channel(ctx, caller, org, m.Channel, ActionRead); err != nil {
		return nil, err
	}
	root := m.ID
	if m.Thread != "" {
		root = m.Thread
	}
	return s.repo.Thread(ctx, org, root)
}

// Search answers the messages matching query in the channels the
// caller may read. An empty query matches nothing.
func (s *service) Search(ctx context.Context, caller platform.Caller, org, query string) ([]Message, error) {
	query = strings.TrimSpace(query)
	channels, err := s.readable(ctx, caller, org)
	if err != nil || query == "" || len(channels) == 0 {
		return nil, err
	}
	return s.repo.Search(ctx, org, ids(channels), query, maxListed)
}

// Linking answers the messages linking a page the caller may read, in
// the channels the caller may read.
func (s *service) Linking(ctx context.Context, caller platform.Caller, org, page string) ([]Message, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.OnPage(platform.ActionPageRead, page)); err != nil {
		return nil, err
	}
	channels, err := s.readable(ctx, caller, org)
	if err != nil || len(channels) == 0 {
		return nil, err
	}
	return s.repo.Linking(ctx, org, ids(channels), page, maxListed)
}

// unique is xs without repeats, in first-seen order.
func unique[T comparable](xs []T) []T {
	var out []T
	for _, x := range xs {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}
