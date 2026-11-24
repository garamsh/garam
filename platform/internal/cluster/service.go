package cluster

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// The typ values of a rotation's two JWSs (ROT-1).
const (
	TypeKeyRotation = "key-rotation+jwt"
	TypeKeyProof    = "key-proof+jwt"
)

// RegistrationsPerSource is the default bound on the registrations one
// client address sends per operator.ExchangeWindow, counted before the
// token is read (ADR-0184 decision 3, REG-8). Config.RegistrationLimit
// replaces it.
const RegistrationsPerSource = 10

// RotationsPerCluster bounds the rotation requests one cluster sends
// per operator.ExchangeWindow, counted before a signature is verified.
// A connector rotates once a key's life and repeats only a lost answer
// (ROT-5), so the bound is far past what one needs.
const RotationsPerCluster = 10

// Config holds the clock and the two absolute URLs a cluster's signed
// requests name as their audience.
type Config struct {
	// Now supplies the current time; zero takes time.Now.
	Now func() time.Time
	// TokenEndpoint is the private_key_jwt exchange's URL, which a
	// registration answers as tokenUrl and an assertion's aud names.
	TokenEndpoint string
	// RotationURL is the key rotation route's absolute URL, which both
	// of a rotation's JWSs name as aud (ROT-1).
	RotationURL string
	// RegistrationLimit bounds the registrations one client address
	// sends per operator.ExchangeWindow; zero takes
	// RegistrationsPerSource. A deployment always has a bound.
	RegistrationLimit int
}

// Service is how a cluster's connector registers, exchanges its key
// for tokens and rotates it (ADR-0184 decisions 3 to 6). A member's
// half, the registration token, is [IssueRegistrationToken].
type Service interface {
	// Register spends token and binds jwk to its cluster, answering
	// the cluster. source is the client's address: past
	// RegistrationsPerSource it answers *operator.Exceeded before the
	// token is read, so a throttled request spends nothing.
	Register(ctx context.Context, source, token string, jwk []byte) (grn.GRN, error)
	// ExchangeAssertion answers a token for the cluster client whose
	// live key signed assertion, under machine-token.v2's profile.
	ExchangeAssertion(ctx context.Context, source string, client grn.GRN, assertion string) (*Token, error)
	// Rotate adds jwk as caller's successor key, proved by
	// authorization, signed by a live key and naming jwk's thumbprint,
	// and proof, signed by jwk (ROT-1 to ROT-4).
	Rotate(ctx context.Context, caller grn.GRN, jwk []byte, authorization, proof string) (Rotation, error)
}

type service struct {
	credentials CredentialStore
	cfg         Config
	// perSource bounds registrations and exchanges per address before
	// anything is read; perClient each cluster's verified exchanges.
	registrations *operator.Limiter
	perSource     *operator.Limiter
	perClient     *operator.Limiter
	// rotations bounds each cluster's rotation requests.
	rotations *operator.Limiter
}

// NewService constructs the service over a cluster's credentials.
func NewService(credentials CredentialStore, cfg Config) Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.RegistrationLimit == 0 {
		cfg.RegistrationLimit = RegistrationsPerSource
	}
	limiter := func(limit int) *operator.Limiter {
		return operator.NewLimiter(operator.LimiterConfig{Limit: limit, Window: operator.ExchangeWindow,
			MaxKeys: operator.TrackedClients, Now: cfg.Now})
	}
	return &service{credentials: credentials, cfg: cfg,
		registrations: limiter(cfg.RegistrationLimit),
		perSource:     limiter(operator.ExchangesPerSource),
		perClient:     limiter(operator.ExchangesPerClient),
		rotations:     limiter(RotationsPerCluster),
	}
}
