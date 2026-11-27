package bot_test

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// tokenURL is garam's audience for a bot's token in these tests.
const tokenURL = "https://machine.garam.test/oauth/token"

// self is the instance whose connector the tests speak for, and
// selfInstallation the installation it is a copy of.
var (
	self             = grn.New("acme", app.InstanceType, "5f60710a1b2c3d4e")
	selfInstallation = grn.New("acme", app.InstallationType, "garam-bot-connector")
)

// hosting answers iam:bot.host for the installations it holds, and
// records each question it was asked.
type hosting struct {
	hosts map[grn.GRN]bool
	asked []policydecision.Question
}

func (h *hosting) Evaluate(_ context.Context, q policydecision.Question) (policydecision.Decision, error) {
	h.asked = append(h.asked, q)
	return policydecision.Decision{Allowed: q.Action == "iam:bot.host" && q.Subject == q.Resource &&
		h.hosts[q.Subject]}, nil
}

func (h *hosting) Evaluations(ctx context.Context, qs []policydecision.Question) ([]policydecision.Decision, error) {
	out := make([]policydecision.Decision, 0, len(qs))
	for _, q := range qs {
		d, _ := h.Evaluate(ctx, q)
		out = append(out, d)
	}
	return out, nil
}
