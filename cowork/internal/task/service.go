package task

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// Service is the record of work as a caller acts on it. Every method
// asks the platform before it reads or writes: listing needs
// cowork:task-read on the organization and on each task listed,
// creating cowork:task-write on the organization, reading a task or
// its comments cowork:task-read on it, and changing or commenting on
// it cowork:task-write on it. Linking a page or a message needs the
// caller to read it.
type Service interface {
	Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Task, error)
	List(ctx context.Context, caller platform.Caller, org string, f Filter) ([]Task, error)
	Get(ctx context.Context, caller platform.Caller, org, id string) (Task, error)
	Update(ctx context.Context, caller platform.Caller, org, id string, in UpdateInput) (Task, error)
	Comment(ctx context.Context, caller platform.Caller, org, id, body string) (Comment, error)
	Comments(ctx context.Context, caller platform.Caller, org, id string) ([]Comment, error)
}

type service struct {
	repo     Repository
	platform platform.Platform
	pages    Pages
	messages Messages
}

// NewService answers the record of work over repo, deciding every act
// through p, and reading what a task links through pages and
// messages.
func NewService(repo Repository, p platform.Platform, pages Pages, messages Messages) Service {
	return &service{repo: repo, platform: p, pages: pages, messages: messages}
}
