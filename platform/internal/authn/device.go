package authn

import (
	"context"
	"crypto/rand"
	"math/big"
	"strings"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// The device grant's bounds (RFC 8628, ADR-0120).
const (
	// DeviceCodeTTL is how long a device code and its user code live.
	DeviceCodeTTL = 10 * time.Minute
	// DevicePollInterval is the interval a CLI is first told to poll at.
	DevicePollInterval = 5 * time.Second
	// DeviceSlowDownStep is what a poll earlier than its code's interval
	// adds to that interval (RFC 8628 §3.5).
	DeviceSlowDownStep = 5 * time.Second
	// MaxLiveDeviceAuthorizations bounds the codes live at one time, and
	// with them the write rate an unauthenticated caller can drive on
	// /auth/device, decided where the write happens as ADR-0030 bounds a
	// login state.
	MaxLiveDeviceAuthorizations = 1024
)

// userCodeAlphabet is RFC 8628 §6.1's: consonants alone, so a code
// spells no word and survives being read aloud.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// userCodeLength is the user code's length without its separator.
const userCodeLength = 8

var (
	// ErrDeviceRateLimited is returned when a device code is issued
	// while MaxLiveDeviceAuthorizations are already live.
	ErrDeviceRateLimited = errorx.New(errorx.KindResourceExhausted, "device authorization issue rate exceeded")
	// ErrDeviceCodeNotPending answers a decision on a user code no live,
	// undecided authorization holds.
	ErrDeviceCodeNotPending = errorx.New(errorx.KindNotFound, "no pending device authorization has that code")
	// ErrDeviceCodeUnknown answers a poll for a device code garam holds
	// no authorization for.
	ErrDeviceCodeUnknown = errorx.New(errorx.KindInvalidArgument, "device code unknown")
)

// DeviceGrant is the pair of codes one device authorization is answered
// with: the device code the CLI polls with, and the user code the
// person approves, formatted XXXX-XXXX.
type DeviceGrant struct {
	DeviceCode string
	UserCode   string
}

// NewDeviceGrant draws a device code and a user code.
func NewDeviceGrant() (DeviceGrant, error) {
	device, err := NewToken()
	if err != nil {
		return DeviceGrant{}, err
	}
	var b strings.Builder
	limit := big.NewInt(int64(len(userCodeAlphabet)))
	for i := range userCodeLength {
		if i == userCodeLength/2 {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return DeviceGrant{}, errorx.Wrap(err, "user code")
		}
		b.WriteByte(userCodeAlphabet[n.Int64()])
	}
	return DeviceGrant{DeviceCode: device, UserCode: b.String()}, nil
}

// NormalizeUserCode reads a user code as a person typed it: case, a
// separator and surrounding space do not matter. It reports false for
// anything no grant could have drawn.
func NormalizeUserCode(typed string) (string, bool) {
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(typed), "-", ""))
	if len(code) != userCodeLength {
		return "", false
	}
	for _, c := range code {
		if !strings.ContainsRune(userCodeAlphabet, c) {
			return "", false
		}
	}
	return code, true
}

// PollOutcome is what one poll of a device code found.
type PollOutcome string

const (
	PollPending  PollOutcome = "authorization_pending"
	PollSlowDown PollOutcome = "slow_down"
	PollDenied   PollOutcome = "access_denied"
	PollExpired  PollOutcome = "expired_token"
	PollIssued   PollOutcome = "issued"
)

// PollResult is a poll's outcome, the interval the code is polled at
// from here, and, for PollIssued, the CLI session it opened.
type PollResult struct {
	Outcome  PollOutcome
	Interval time.Duration
	Session  *Session
}

// DeviceStore persists device authorizations by their codes' hashes
// (ADR-0120).
type DeviceStore interface {
	// Issue records grant as live for DeviceCodeTTL, polled at
	// DevicePollInterval, or returns ErrDeviceRateLimited when
	// MaxLiveDeviceAuthorizations are already live. clientName, where
	// it is not empty, is what the CLI said it runs on.
	Issue(ctx context.Context, grant DeviceGrant, clientName string) error
	// Decide approves or denies the live, undecided authorization
	// userCode names, as user, or returns ErrDeviceCodeNotPending.
	Decide(ctx context.Context, userCode string, user grn.GRN, approve bool) error
	// Poll reads the authorization deviceCode names, and for an
	// approved one opens its CLI session under tokens and spends the
	// authorization in the same transaction, so a code is redeemed
	// once. It returns ErrDeviceCodeUnknown for a code it holds nothing
	// for.
	Poll(ctx context.Context, deviceCode string, tokens CLITokens) (PollResult, error)
}
