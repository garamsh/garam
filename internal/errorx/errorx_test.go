package errorx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/errorx"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		kind    errorx.Kind
		want    errorx.Kind
		message string
	}{
		{"not_found", errorx.KindNotFound, errorx.KindNotFound, "user not found"},
		{"already_exists", errorx.KindAlreadyExists, errorx.KindAlreadyExists, "user exists"},
		{"permission_denied", errorx.KindPermissionDenied, errorx.KindPermissionDenied, "forbidden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := errorx.New(tc.kind, tc.message)
			assert.Equal(t, tc.want, errorx.KindOf(err))
			assert.Equal(t, tc.message, err.Error())
			assert.NoError(t, errors.Unwrap(err))
		})
	}
}

func TestWrap_NilReturnsNil(t *testing.T) {
	assert.Nil(t, errorx.Wrap(nil, "ignored"))
}

func TestWrap_KindFromChain(t *testing.T) {
	inner := errorx.New(errorx.KindNotFound, "user not found")
	outer := errorx.Wrap(inner, "user lookup")

	assert.True(t, errorx.IsKind(outer, errorx.KindNotFound))
	assert.Equal(t, "user lookup: user not found", outer.Error())
}

func TestWrap_KindOverride(t *testing.T) {
	inner := errorx.New(errorx.KindInternal, "boom")
	outer := errorx.Wrap(inner, "user create", errorx.KindAlreadyExists)

	assert.True(t, errorx.IsKind(outer, errorx.KindAlreadyExists))
	assert.Same(t, inner, errors.Unwrap(outer))
}

func TestKindOf_UnknownForPlainError(t *testing.T) {
	plain := errors.New("plain")
	assert.Equal(t, errorx.KindUnknown, errorx.KindOf(plain))
	assert.False(t, errorx.IsKind(plain, errorx.KindNotFound))
}

func TestKindOf_DeepChain(t *testing.T) {
	inner := errorx.New(errorx.KindPermissionDenied, "denied")
	mid := fmt.Errorf("mid: %w", inner)
	outer := errorx.Wrap(mid, "service call")

	require.True(t, errorx.IsKind(outer, errorx.KindPermissionDenied))
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		k    errorx.Kind
		want string
	}{
		{errorx.KindNotFound, "not_found"},
		{errorx.KindAlreadyExists, "already_exists"},
		{errorx.KindPermissionDenied, "permission_denied"},
		{errorx.KindUnauthenticated, "unauthenticated"},
		{errorx.KindInternal, "internal"},
		{errorx.KindUnknown, "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.k.String())
		})
	}
}
