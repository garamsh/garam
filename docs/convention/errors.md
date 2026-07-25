# Error Conventions

All errors flow through [`internal/errorx`](../../internal/errorx/errorx.go).
Kinds align with gRPC status codes so the same error crosses any
transport.

## Use

```go
// Sentinel — declared once in the owning domain package.
var ErrUserNotFound = errorx.New(errorx.KindNotFound, "user not found")

// Wrap at the boundary.
if err != nil {
    return errorx.Wrap(err, "user lookup")
}

// Override kind explicitly when needed.
return errorx.Wrap(err, "user create", errorx.KindAlreadyExists)
```

- `errorx.New(kind, msg)` — new error with a fixed kind.
- `errorx.Wrap(err, msg, kind...)` — attach context. Falls back to the
  chain's kind when no override is given.
- `errorx.IsKind(err, kind)` — kind check across the chain. Don't mix
  with `errors.Is`.

## Placement

| Kind | Location | Example |
|------|----------|---------|
| Domain sentinel | `internal/<domain>/<domain>.go` | `ErrUserNotFound` |
| Shared sentinel | `internal/errorx/` | none yet |
| Transport mapping | transport layer only | HTTP/gRPC handlers |

## Forbidden

- `errors.New(...)` — loses the kind.
- `fmt.Errorf("...: %w", err)` — kind drifts through the chain.
- Panic in library code.
- Log and return at the same site — pick one (usually return).

Do not add new kinds. Map at the transport boundary if you need more.