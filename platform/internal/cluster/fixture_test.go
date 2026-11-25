package cluster_test

import (
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
)

// tokenURL is garam's audience for a bot's token in these tests.
const tokenURL = "https://machine.garam.test/oauth/token"

// self is the instance whose connector the tests speak for.
var self = grn.New("acme", app.InstanceType, "5f60710a1b2c3d4e")
