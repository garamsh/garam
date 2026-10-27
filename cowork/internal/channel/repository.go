package channel

import (
	"context"

	"github.com/garamsh/garam/platform/sdk/grn"
)

// Repository keeps channels, their lists and their messages. Every
// method is scoped to an organization.
type Repository interface {
	// Create stores c with its list. A name the organization already
	// has is ErrNameTaken.
	Create(ctx context.Context, c Channel) error
	Get(ctx context.Context, org, id string) (Channel, error)
	// List answers the organization's public channels and every
	// channel whose list holds member.
	List(ctx context.Context, org string, member grn.GRN) ([]Channel, error)
	AddMember(ctx context.Context, org, id string, member grn.GRN) error
	RemoveMember(ctx context.Context, org, id string, member grn.GRN) error

	// Post stores m, and for a reply checks that its thread is in the
	// same channel and starts no other: a reply to a reply is stored
	// under the first message of the thread. It answers m as stored.
	Post(ctx context.Context, m Message) (Message, error)
	GetMessage(ctx context.Context, org, id string) (Message, error)
	// Messages answers a channel's messages that are not replies,
	// oldest first, the last limit of them before before (all, where
	// before is empty), each with its reply count.
	Messages(ctx context.Context, org, channel, before string, limit int) ([]Message, error)
	// Thread answers a thread's first message and its replies, oldest
	// first.
	Thread(ctx context.Context, org, id string) ([]Message, error)
	// Search answers the messages in channels whose text matches query,
	// newest first, at most limit.
	Search(ctx context.Context, org string, channels []string, query string, limit int) ([]Message, error)
	// Linking answers the messages in channels that link page, newest
	// first, at most limit.
	Linking(ctx context.Context, org string, channels []string, page string, limit int) ([]Message, error)
}
