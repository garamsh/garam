package bot

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// ActionHost is what an installation holds where its owner granted it
// bot hosting (ADR-0192 decision 10), decided with the installation as
// principal and as resource. No role holds it, so it is the bot
// domain's and not among org.Actions.
const ActionHost = "iam:bot.host"

// ErrNotHosting answers an instance whose installation its owner has
// not granted bot hosting, as a declaration's target or as a bot
// exchange's actor (ADR-0192 decision 10).
var ErrNotHosting = errorx.New(errorx.KindPermissionDenied, "the instance's installation is not granted bot hosting")

// Hosts refuses with ErrNotHosting an installation that does not hold
// iam:bot.host on itself. A question the policy service could not
// answer is its error, which refuses as well.
func Hosts(ctx context.Context, decisions policydecision.Service, installation grn.GRN) error {
	if decisions == nil {
		return errorx.New(errorx.KindUnavailable, "bot: no policy service decides bot hosting")
	}
	decision, err := decisions.Evaluate(ctx, policydecision.Question{
		Subject: installation, Action: ActionHost, Resource: installation,
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return ErrNotHosting
	}
	return nil
}
