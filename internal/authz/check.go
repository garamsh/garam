package authz

import "context"

// Checker decides whether a subject may perform an action on an object.
type Checker interface {
	Allow(ctx context.Context, subject Subject, object Object, action string) (bool, error)
}
