// Package memory implements channel.Repository in memory, for tests and
// for a server run without a database.
package memory

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/platform/sdk/grn"
)

// Repository holds every organization's channels and messages behind
// one lock.
type Repository struct {
	mu       sync.Mutex
	channels map[string]channel.Channel // by org + "/" + id
	messages []channel.Message          // in the order posted
}

var _ channel.Repository = (*Repository)(nil)

// New answers an empty repository.
func New() *Repository {
	return &Repository{channels: map[string]channel.Channel{}}
}

func key(org, id string) string { return org + "/" + id }

// Create implements channel.Repository.
func (r *Repository) Create(_ context.Context, c channel.Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, other := range r.channels {
		if other.Org == c.Org && c.Kind != channel.KindDirect && other.Kind != channel.KindDirect && other.Name == c.Name {
			return channel.ErrNameTaken
		}
	}
	c.Members = slices.Clone(c.Members)
	r.channels[key(c.Org, c.ID)] = c
	return nil
}

// Get implements channel.Repository.
func (r *Repository) Get(_ context.Context, org, id string) (channel.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.channels[key(org, id)]
	if !ok {
		return channel.Channel{}, channel.ErrChannelNotFound
	}
	c.Members = slices.Clone(c.Members)
	return c, nil
}

// List implements channel.Repository.
func (r *Repository) List(_ context.Context, org string, member grn.GRN) ([]channel.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []channel.Channel
	for _, c := range r.channels {
		if c.Org == org && (!c.Lists() || c.HasMember(member)) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b channel.Channel) int { return strings.Compare(a.Name+a.ID, b.Name+b.ID) })
	return out, nil
}

func (r *Repository) setMembers(org, id string, change func([]grn.GRN) []grn.GRN) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.channels[key(org, id)]
	if !ok {
		return channel.ErrChannelNotFound
	}
	c.Members = change(c.Members)
	r.channels[key(org, id)] = c
	return nil
}

// AddMember implements channel.Repository.
func (r *Repository) AddMember(_ context.Context, org, id string, member grn.GRN) error {
	return r.setMembers(org, id, func(ms []grn.GRN) []grn.GRN {
		if slices.Contains(ms, member) {
			return ms
		}
		return append(ms, member)
	})
}

// RemoveMember implements channel.Repository.
func (r *Repository) RemoveMember(_ context.Context, org, id string, member grn.GRN) error {
	return r.setMembers(org, id, func(ms []grn.GRN) []grn.GRN {
		return slices.DeleteFunc(ms, func(g grn.GRN) bool { return g == member })
	})
}

func (r *Repository) find(org, id string) (channel.Message, bool) {
	for _, m := range r.messages {
		if m.Org == org && m.ID == id {
			return m, true
		}
	}
	return channel.Message{}, false
}

// Post implements channel.Repository.
func (r *Repository) Post(_ context.Context, m channel.Message) (channel.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.channels[key(m.Org, m.Channel)]; !ok {
		return channel.Message{}, channel.ErrChannelNotFound
	}
	if m.Thread != "" {
		root, ok := r.find(m.Org, m.Thread)
		if !ok || root.Channel != m.Channel {
			return channel.Message{}, channel.ErrMessageNotFound
		}
		if root.Thread != "" {
			m.Thread = root.Thread
		}
	}
	r.messages = append(r.messages, m)
	return m, nil
}

// GetMessage implements channel.Repository.
func (r *Repository) GetMessage(_ context.Context, org, id string) (channel.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.find(org, id)
	if !ok {
		return channel.Message{}, channel.ErrMessageNotFound
	}
	return m, nil
}

func (r *Repository) replies(org, id string) int {
	n := 0
	for _, m := range r.messages {
		if m.Org == org && m.Thread == id {
			n++
		}
	}
	return n
}

// Messages implements channel.Repository.
func (r *Repository) Messages(_ context.Context, org, id, before string, limit int) ([]channel.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []channel.Message
	for _, m := range r.messages {
		if m.Org != org || m.Channel != id || m.Thread != "" {
			continue
		}
		if m.ID == before {
			break
		}
		m.Replies = r.replies(org, m.ID)
		out = append(out, m)
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// Thread implements channel.Repository.
func (r *Repository) Thread(_ context.Context, org, id string) ([]channel.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []channel.Message
	for _, m := range r.messages {
		if m.Org == org && (m.ID == id || m.Thread == id) {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil, channel.ErrMessageNotFound
	}
	return out, nil
}

// newest answers the messages in channels that match, newest first.
func (r *Repository) newest(org string, channels []string, limit int, match func(channel.Message) bool) []channel.Message {
	var out []channel.Message
	for i := len(r.messages) - 1; i >= 0 && len(out) < limit; i-- {
		m := r.messages[i]
		if m.Org == org && slices.Contains(channels, m.Channel) && match(m) {
			out = append(out, m)
		}
	}
	return out
}

// Search implements channel.Repository, matching every word of query
// in the body, case aside.
func (r *Repository) Search(_ context.Context, org string, channels []string, query string, limit int) ([]channel.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	words := strings.Fields(strings.ToLower(query))
	return r.newest(org, channels, limit, func(m channel.Message) bool {
		body := strings.ToLower(m.Body)
		for _, w := range words {
			if !strings.Contains(body, w) {
				return false
			}
		}
		return true
	}), nil
}

// Linking implements channel.Repository.
func (r *Repository) Linking(_ context.Context, org string, channels []string, page string, limit int) ([]channel.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.newest(org, channels, limit, func(m channel.Message) bool { return slices.Contains(m.Pages, page) }), nil
}
