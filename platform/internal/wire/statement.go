package wire

import (
	"github.com/garamsh/garam/internal/errorx"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// StatementToProto renders one statement a caller collected as the
// authz contract's Statement. The fields are taken one by one rather
// than as a type, because each listener declares the statement its own
// Evaluator seam is asked with (ADR-0017, ADR-0024) and this package
// names only the format. An Effect neither constant names is refused
// as [EffectToProto] refuses it, naming the statement.
func StatementToProto(sid string, effect Effect, action, resource []string) (*authzv1.Statement, error) {
	e, err := EffectToProto(effect)
	if err != nil {
		return nil, errorx.Wrap(err, "statement "+sid)
	}
	return &authzv1.Statement{Sid: sid, Effect: e, Action: action, Resource: resource}, nil
}
