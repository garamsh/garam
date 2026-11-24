package cluster

import (
	"context"
	"errors"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// Rotate verifies both JWSs before the store is asked, so the bearer
// token alone reaches no write (ROT-2). Every way they fail to prove
// both keys answers ErrInvalidProof. Each cluster's rotations are
// bounded before any signature is verified, since each verifies two.
func (s *service) Rotate(ctx context.Context, caller grn.GRN, jwk []byte, authorization, proof string) (Rotation, error) {
	if s.cfg.RotationURL == "" {
		return Rotation{}, ErrRotationUnavailable
	}
	if !IsClusterGRN(caller) {
		return Rotation{}, ErrInvalidProof
	}
	if err := s.rotations.Check(caller.String()); err != nil {
		return Rotation{}, err
	}
	successor, err := ParsePublicJWK(jwk)
	if err != nil {
		return Rotation{}, err
	}
	authorizing, err := operator.ParseSignedRequest(authorization, TypeKeyRotation)
	if err != nil {
		return Rotation{}, ErrInvalidProof
	}
	current, err := s.credentials.VerificationKey(ctx, caller, authorizing.KID())
	if errors.Is(err, operator.ErrClientInvalid) {
		return Rotation{}, ErrInvalidProof
	}
	if err != nil {
		return Rotation{}, err
	}
	authorized, err := authorizing.VerifyRequest(current.Verifier(), caller, s.cfg.RotationURL,
		map[string]string{"jkt": successor.KID()})
	if err != nil {
		return Rotation{}, ErrInvalidProof
	}
	proving, err := operator.ParseSignedRequest(proof, TypeKeyProof)
	if err != nil || proving.KID() != successor.KID() {
		return Rotation{}, ErrInvalidProof
	}
	proved, err := proving.VerifyRequest(Key{Cluster: caller, KID: successor.KID(), Public: successor.Public()}.Verifier(),
		caller, s.cfg.RotationURL, nil)
	if err != nil {
		return Rotation{}, ErrInvalidProof
	}
	return s.credentials.Rotate(ctx, caller, current.ID, successor, []operator.VerifiedAssertion{authorized, proved})
}
