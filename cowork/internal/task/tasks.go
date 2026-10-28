package task

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

// valid reports whether t's fields are within the limits.
func (t Task) valid() bool {
	if t.Title == "" || len(t.Title) > maxTitle || len(t.Description) > maxDescription || !t.Status.valid() {
		return false
	}
	if len(t.Pages) > maxLinks || len(t.Messages) > maxLinks {
		return false
	}
	if t.Due != "" {
		if _, err := time.Parse(DateLayout, t.Due); err != nil {
			return false
		}
	}
	return true
}

func validAssignee(g grn.GRN) error {
	if g.IsZero() {
		return nil
	}
	if g.Validate() != nil || (g.ServiceType != "user" && g.ServiceType != "agent") {
		return ErrInvalidAssignee
	}
	return nil
}

// distinct answers ids without repeats, in order.
func distinct(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// readable answers nil when the caller may read every page and message
// in pages and messages: the ones a task it writes may link.
func (s *service) readable(ctx context.Context, caller platform.Caller, org string, pages, messages []string) error {
	for _, id := range pages {
		if _, err := s.pages.Get(ctx, caller, org, id); err != nil {
			return errorx.Wrap(err, "linked page "+id, errorx.KindOf(err))
		}
	}
	for _, id := range messages {
		if _, err := s.messages.Thread(ctx, caller, org, id); err != nil {
			return errorx.Wrap(err, "linked message "+id, errorx.KindOf(err))
		}
	}
	return nil
}

// Create adds a task, as the caller.
func (s *service) Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Task, error) {
	now := time.Now()
	t := Task{
		ID: newID(), Org: org, Title: strings.TrimSpace(in.Title), Description: in.Description,
		Status: in.Status, Assignee: in.Assignee, Due: in.Due, CreatedBy: caller.GRN, CreatedAt: now, UpdatedAt: now,
		Pages: distinct(in.Pages), Messages: distinct(in.Messages),
	}
	if t.Status == "" {
		t.Status = StatusTodo
	}
	if !t.valid() {
		return Task{}, ErrInvalidTask
	}
	if err := validAssignee(t.Assignee); err != nil {
		return Task{}, err
	}
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: ActionWrite}); err != nil {
		return Task{}, err
	}
	if err := s.readable(ctx, caller, org, t.Pages, t.Messages); err != nil {
		return Task{}, err
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return Task{}, err
	}
	return t, nil
}

// maxChecksPerAsk is how many pairs the platform answers in one
// permission check request.
const maxChecksPerAsk = 50

// List answers the tasks matching f the caller may read.
func (s *service) List(ctx context.Context, caller platform.Caller, org string, f Filter) ([]Task, error) {
	if f.Status != "" && !f.Status.valid() {
		return nil, ErrInvalidTask
	}
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: ActionRead}); err != nil {
		return nil, err
	}
	listed, err := s.repo.List(ctx, org, f, maxListed)
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for start := 0; start < len(listed); start += maxChecksPerAsk {
		batch := listed[start:min(start+maxChecksPerAsk, len(listed))]
		checks := make([]platform.Check, len(batch))
		for i, t := range batch {
			checks[i] = platform.OnTask(ActionRead, t.ID)
		}
		allowed, err := s.platform.Allowed(ctx, caller, org, checks)
		if err != nil {
			return nil, err
		}
		for i, t := range batch {
			if allowed[i] {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

// Get answers one task the caller may read.
func (s *service) Get(ctx context.Context, caller platform.Caller, org, id string) (Task, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.OnTask(ActionRead, id)); err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, org, id)
}

// Update changes a task the caller may write. A link it adds must be
// one the caller may read; a link it keeps is not asked again.
func (s *service) Update(ctx context.Context, caller platform.Caller, org, id string, in UpdateInput) (Task, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.OnTask(ActionWrite, id)); err != nil {
		return Task{}, err
	}
	t, err := s.repo.Get(ctx, org, id)
	if err != nil {
		return Task{}, err
	}
	var addedPages, addedMessages []string
	if in.Title != nil {
		t.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Status != nil {
		t.Status = *in.Status
	}
	if in.Assignee != nil {
		if err := validAssignee(*in.Assignee); err != nil {
			return Task{}, err
		}
		t.Assignee = *in.Assignee
	}
	if in.Due != nil {
		t.Due = *in.Due
	}
	if in.Pages != nil {
		next := distinct(*in.Pages)
		addedPages = added(t.Pages, next)
		t.Pages = next
	}
	if in.Messages != nil {
		next := distinct(*in.Messages)
		addedMessages = added(t.Messages, next)
		t.Messages = next
	}
	if !t.valid() {
		return Task{}, ErrInvalidTask
	}
	if err := s.readable(ctx, caller, org, addedPages, addedMessages); err != nil {
		return Task{}, err
	}
	t.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, t); err != nil {
		return Task{}, err
	}
	return t, nil
}

// added answers the IDs next holds that was did not.
func added(was, next []string) []string {
	var out []string
	for _, id := range next {
		if !slices.Contains(was, id) {
			out = append(out, id)
		}
	}
	return out
}
