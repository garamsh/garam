// Package channel is cowork's messaging: an organization's channels,
// public or private, its direct messages, and the messages and threads
// in them, kept as the record of the conversation (ADR-0127). Who may
// read and post is the platform's to allow, and a private channel's or
// a direct message's participants narrow that further (ADR-0128).
package channel

import (
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// ServiceType is a channel's GRN service type, the kind the platform's
// permission checks name it by. A direct message is a channel too.
const ServiceType = "cowork-channel"

// Kind is who a channel is open to, within what the platform allows.
type Kind string

const (
	// KindPublic is open to every member the platform allows.
	KindPublic Kind = "public"
	// KindPrivate is open to its participants, who may add others.
	KindPrivate Kind = "private"
	// KindDirect is between the people it started with, and has no
	// name.
	KindDirect Kind = "direct"
)

// Channel is one channel of an organization.
type Channel struct {
	ID        string
	Org       string
	Kind      Kind
	Name      string
	CreatedBy grn.GRN
	CreatedAt time.Time
	// Members are a private channel's or a direct message's
	// participants. A public channel has none.
	Members []grn.GRN
}

// GRN is the channel's resource name under its organization.
func (c Channel) GRN() grn.GRN {
	return grn.New(c.Org, grn.DefaultProject, ServiceType, c.ID)
}

// Lists reports whether the channel keeps a participant list.
func (c Channel) Lists() bool { return c.Kind != KindPublic }

// HasMember reports whether g is on the channel's list.
func (c Channel) HasMember(g grn.GRN) bool {
	for _, m := range c.Members {
		if m == g {
			return true
		}
	}
	return false
}

// Message is one message, a channel's or a thread's. Thread is the ID
// of the message that starts its thread, empty for a message that is
// not a reply.
type Message struct {
	ID      string
	Org     string
	Channel string
	Thread  string
	Author  grn.GRN
	Body    string
	// Mentions are the people and agents the message names.
	Mentions []grn.GRN
	// Pages are the IDs of the pages the message links.
	Pages []string
	// Artifacts are the IDs of the artifacts the message embeds.
	Artifacts []string
	CreatedAt time.Time
	// Replies is how many messages its thread holds, for a message
	// listed in its channel.
	Replies int
}

// UndeliveredMentions are the agents a message names. Reaching an
// agent's runtime waits on the principal the platform issues cowork
// (#1664), so every one is undelivered.
func (m Message) UndeliveredMentions() []grn.GRN {
	var out []grn.GRN
	for _, g := range m.Mentions {
		if g.ServiceType == "agent" {
			out = append(out, g)
		}
	}
	return out
}

// CreateInput is a new channel. A direct message takes no name, and
// its Members are everyone in it besides its creator.
type CreateInput struct {
	Kind    Kind
	Name    string
	Members []grn.GRN
}

// PostInput is a new message. Thread names the message to reply to;
// a reply to a reply joins the same thread.
type PostInput struct {
	Body      string
	Thread    string
	Mentions  []grn.GRN
	Pages     []string
	Artifacts []string
}

// Limits on what a caller sends.
const (
	maxName     = 80
	maxBody     = 40000
	maxMentions = 50
	maxPages    = 20
	// maxArtifacts is the most artifacts one message embeds.
	maxArtifacts = 10
	// maxListed is the most messages one read answers.
	maxListed = 200
)

var (
	ErrChannelNotFound = errorx.New(errorx.KindNotFound, "channel not found")
	ErrMessageNotFound = errorx.New(errorx.KindNotFound, "message not found")
	ErrNameTaken       = errorx.New(errorx.KindAlreadyExists, "a channel of the organization has that name")
	ErrInvalidChannel  = errorx.New(errorx.KindInvalidArgument, "a channel has a name of 1 to 80 bytes, and a direct message has none and names who it is with")
	ErrInvalidMessage  = errorx.New(errorx.KindInvalidArgument, "a message has a body of 1 to 40000 bytes, at most 50 mentions, 20 pages and 10 artifacts")
	// ErrNotParticipant is a caller the platform allows who is not on
	// the channel's list (ADR-0128).
	ErrNotParticipant = errorx.New(errorx.KindPermissionDenied, "not a participant of this channel")
	// ErrPublicHasNoList is adding to a channel that keeps no list.
	ErrPublicHasNoList = errorx.New(errorx.KindFailedPrecondition, "a public channel has no participants to add")
)
