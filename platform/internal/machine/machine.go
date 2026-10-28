// Package machine authenticates a machine identity — an agent, an
// operator or a standalone host's supervisor — by the token a request
// presents (ADR-0100, ADR-0118).
//
// What the token asserts is the GRN it was issued to and nothing else:
// no user row, no session, no organization membership. Its serial and
// dates travel beside that GRN, which is what the execution fence
// judges an agent's request by. The listener [NewServer] builds
// terminates TLS itself and asks for no client certificate; a request
// presenting no token carries no [Identity] rather than being refused,
// and what it may reach is the handler's to decide.
//
// What it presents in the other direction is one certificate every
// machine identity pins, asked for per handshake through
// [ServerCertificate] so a replaced one is served without a restart
// (ADR-0040).
package machine

import (
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// ErrOutsideValidity refuses an identity whose token's dates no longer
// hold when a decision reads them again.
var ErrOutsideValidity = errorx.New(errorx.KindPermissionDenied,
	"machine: the token is outside its validity period")

// Identity is the machine identity the token a request presented
// stands for. Only the listener's token read constructs one, so an
// Identity holds a GRN whose token was live, and whose record held,
// when its request was read; [Recheck] is what asks them again for a
// later decision.
//
// The GRN is the whole of what the token asserts — no roles, no
// claims. The three fields below describe the token itself.
type Identity struct {
	grn       grn.GRN
	serial    string
	notBefore time.Time
	notAfter  time.Time
}

// GRN returns the identity's GRN, which is the whole of what the token
// asserts.
func (i Identity) GRN() grn.GRN { return i.grn }

// Serial returns the token's serial, which is what tells it from
// another token naming the same identity. An operator's token has
// none.
func (i Identity) Serial() string { return i.serial }

// NotBefore returns when the token was issued.
func (i Identity) NotBefore() time.Time { return i.notBefore }

// NotAfter returns when the token stops being accepted.
func (i Identity) NotAfter() time.Time { return i.notAfter }
