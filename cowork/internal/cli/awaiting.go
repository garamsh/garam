package cli

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/artifact"
	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/cowork/internal/policyfeed"
	"github.com/garamsh/garam/cowork/internal/task"
)

// The services below answer a create, and a delete of a page, after the
// policy service holds the change (ADR-0166). Each waits by the entity
// it names, after the write is committed and holding no transaction,
// and only where the write succeeded. Every other method is the
// service's own: an update to a held entity changes nothing the service
// holds.

type awaitingPages struct {
	page.Service
	gate policyfeed.Gate
}

func (s awaitingPages) Create(ctx context.Context, caller platform.Caller, org string, in page.CreateInput) (page.Page, error) {
	p, err := s.Service.Create(ctx, caller, org, in)
	if err != nil {
		return p, err
	}
	return p, s.gate.Await(ctx, policyfeed.Entity{Type: "page", Org: p.Org, ID: p.ID})
}

// Delete waits on the mark of every page the delete removed: the page
// and the pages beneath it, which go with it by foreign key and raise
// marks of their own in the same transaction. Each wait has the gate's
// bound and the first to pass it answers the request.
func (s awaitingPages) Delete(ctx context.Context, caller platform.Caller, org, id string) ([]string, error) {
	removed, err := s.Service.Delete(ctx, caller, org, id)
	if err != nil {
		return nil, err
	}
	for _, at := range removed {
		if err := s.gate.Await(ctx, policyfeed.Entity{Type: "page", Org: org, ID: at}); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

type awaitingChannels struct {
	channel.Service
	gate policyfeed.Gate
}

func (s awaitingChannels) Create(ctx context.Context, caller platform.Caller, org string, in channel.CreateInput) (channel.Channel, error) {
	c, err := s.Service.Create(ctx, caller, org, in)
	if err != nil {
		return c, err
	}
	return c, s.gate.Await(ctx, policyfeed.Entity{Type: "channel", Org: c.Org, ID: c.ID})
}

type awaitingTasks struct {
	task.Service
	gate policyfeed.Gate
}

func (s awaitingTasks) Create(ctx context.Context, caller platform.Caller, org string, in task.CreateInput) (task.Task, error) {
	t, err := s.Service.Create(ctx, caller, org, in)
	if err != nil {
		return t, err
	}
	return t, s.gate.Await(ctx, policyfeed.Entity{Type: "task", Org: t.Org, ID: t.ID})
}

type awaitingArtifacts struct {
	artifact.Service
	gate policyfeed.Gate
}

func (s awaitingArtifacts) Create(ctx context.Context, caller platform.Caller, org string, in artifact.CreateInput) (artifact.Artifact, error) {
	a, err := s.Service.Create(ctx, caller, org, in)
	if err != nil {
		return a, err
	}
	return a, s.gate.Await(ctx, policyfeed.Entity{Type: "artifact", Org: a.Org, ID: a.ID})
}
