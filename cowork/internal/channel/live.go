package channel

import (
	"context"
	"encoding/json"

	"github.com/garamsh/garam/cowork/internal/fanout"
	"github.com/garamsh/garam/cowork/internal/platform"
)

// liveMessage is a message as a channel's listeners receive it: the
// same fields cowork's API answers for one (api/openapi.yaml,
// `Message`), so a client renders both with one type.
type liveMessage struct {
	ID                  string   `json:"id"`
	Channel             string   `json:"channel"`
	Thread              string   `json:"thread"`
	Author              string   `json:"author"`
	Body                string   `json:"body"`
	Mentions            []string `json:"mentions"`
	UndeliveredMentions []string `json:"undeliveredMentions"`
	Pages               []string `json:"pages"`
	CreatedAt           string   `json:"createdAt"`
	Replies             int      `json:"replies"`
}

// Live is m as a listener receives it: one JSON object.
func Live(m Message) []byte {
	strs := func(n int, at func(int) string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = at(i)
		}
		return out
	}
	undelivered := m.UndeliveredMentions()
	raw, _ := json.Marshal(liveMessage{
		ID: m.ID, Channel: m.Channel, Thread: m.Thread, Author: m.Author.String(), Body: m.Body,
		Mentions:            strs(len(m.Mentions), func(i int) string { return m.Mentions[i].String() }),
		UndeliveredMentions: strs(len(undelivered), func(i int) string { return undelivered[i].String() }),
		Pages:               append([]string{}, m.Pages...),
		CreatedAt:           m.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Replies:             m.Replies,
	})
	return raw
}

// Listen implements Service. Reading is decided once, when the caller
// starts listening; a change of policy reaches a listener when it
// listens again.
func (s *service) Listen(ctx context.Context, caller platform.Caller, org, id string, peer fanout.Peer) (func(), error) {
	if _, err := s.channel(ctx, caller, org, id, ActionRead); err != nil {
		return nil, err
	}
	return s.hub.Join(room(org, id), peer), nil
}
