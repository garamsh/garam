package bot

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
)

// BotTokenStore keeps the tokens a connector exchanged for the bots
// placed on its instance (ADR-0189 decision 2). A token goes with its
// instance, its declaration and the withdrawal of the key its actor's
// token was exchanged under.
type BotTokenStore interface {
	// RecordBotToken stores hash as a bot's token for g, issued for the
	// live Bot declaration id places on g, under the key the actor
	// token actorHash was exchanged under, and accepted for ttl from the
	// database's time; it answers when the token expires. It holds g's
	// record, so no tombstone or key revocation lands meanwhile, and
	// sweeps g's expired bot tokens. A declaration that is not a live
	// Bot of g is ErrBotNotPlaced, and an actor token no longer live is
	// app.ErrTokenInvalid.
	RecordBotToken(ctx context.Context, g grn.GRN, id, actorHash, hash string, ttl time.Duration) (time.Time, error)
}
