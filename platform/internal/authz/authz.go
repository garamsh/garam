package authz

import "github.com/garamsh/garam/platform/internal/errorx"

// Policy sentinel errors.
var (
	ErrPolicyNotFound          = errorx.New(errorx.KindNotFound, "policy not found")
	ErrPolicyVersionNotFound   = errorx.New(errorx.KindNotFound, "policy version not found")
	ErrPolicyAlreadyExists     = errorx.New(errorx.KindAlreadyExists, "policy already exists")
	ErrAttachmentNotFound      = errorx.New(errorx.KindNotFound, "policy attachment not found")
	ErrAttachmentAlreadyExists = errorx.New(errorx.KindAlreadyExists, "policy attachment already exists")
	ErrLastAttachment          = errorx.New(errorx.KindFailedPrecondition, "detaching an active policy's last attachment applies it org-wide; delete the policy or attach another subject first")
)
