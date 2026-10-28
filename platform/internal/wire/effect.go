// Package wire holds the conversions between the shared primitives
// (errorx kinds, GRNs, the statement Effect, a policy's status) and
// the wire formats the request surfaces share: the authz gRPC contract
// and HTTP status codes. api, machineapi and authz each import it and
// never import each other (ADR-0003, ADR-0017); what every surface
// converts the same way is declared once, here.
package wire

import (
	"github.com/garamsh/garam/platform/internal/errorx"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// Effect is a statement's outcome when it matches (ADR-0002). One
// declaration serves every surface; the engine owns the semantics.
type Effect string

const (
	EffectAllow Effect = "Allow"
	EffectDeny  Effect = "Deny"
)

// EffectToProto renders an Effect on the wire. An Effect that is
// neither constant is refused: the process that built the statement
// owns the defect, so it is internal, not the caller's.
func EffectToProto(e Effect) (authzv1.Effect, error) {
	switch e {
	case EffectAllow:
		return authzv1.Effect_EFFECT_ALLOW, nil
	case EffectDeny:
		return authzv1.Effect_EFFECT_DENY, nil
	default:
		return authzv1.Effect_EFFECT_UNSPECIFIED, errorx.New(errorx.KindInternal, "effect must be Allow or Deny")
	}
}

// EffectFromProto parses a wire Effect. Anything outside the two
// constants is the caller's error.
func EffectFromProto(e authzv1.Effect) (Effect, error) {
	switch e {
	case authzv1.Effect_EFFECT_ALLOW:
		return EffectAllow, nil
	case authzv1.Effect_EFFECT_DENY:
		return EffectDeny, nil
	default:
		return "", errorx.New(errorx.KindInvalidArgument, "effect must be ALLOW or DENY")
	}
}
