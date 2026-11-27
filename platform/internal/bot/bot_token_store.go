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
	// token actorHash was exchanged under, bound to the key jkt names,
	// and accepted for ttl from the database's time; it answers when
	// the token expires. It holds g's
	// record, so no tombstone or key revocation lands meanwhile, and
	// sweeps g's expired bot tokens. A declaration that is not a live
	// Bot of g is ErrBotNotPlaced, and an actor token no longer live is
	// app.ErrTokenInvalid.
	RecordBotToken(ctx context.Context, g grn.GRN, id, actorHash, hash, jkt string, ttl time.Duration) (time.Time, error)
	// ResolveBotToken answers the live bot token whose hash is hash,
	// with the key it is bound to, or ErrBotTokenInvalid: one that
	// expired, or whose connector key is revoked or past its rollover,
	// is not live.
	ResolveBotToken(ctx context.Context, hash string) (HeldBotToken, error)
	// RevokeInstallationBotTokens deletes every bot token minted for an
	// instance of installation, as its owner's withdrawal of bot
	// hosting takes them (ADR-0192 decision 10).
	RevokeInstallationBotTokens(ctx context.Context, installation grn.GRN) error
}
