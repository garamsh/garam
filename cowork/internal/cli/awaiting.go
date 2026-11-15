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

// The services below answer a create after the policy service holds
// the entity it created (ADR-0166). Each waits by the entity it names,
// after the write is committed and holding no transaction, and only
// where the create succeeded. Every other method is the service's own:
// a change to a held entity changes nothing the service holds.

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
