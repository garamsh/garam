# Go Conventions

Based on Effective Go and Code Review Comments. Only project-specific
decisions are recorded here.

## Layout

```
<module>/
├── cmd/<binary>/main.go
├── internal/
│   ├── <domain>/
│   │   ├── <domain>.go     # aggregate, sentinel errors
│   │   ├── service.go      # Service interface + impl + NewService
│   │   ├── store.go        # Store interface
│   │   ├── postgres.go | memory.go | redis.go   # impl
│   │   └── mocks/          # mockery v3 output
│   └── <crosscutting>/     # only when ≥3 domains depend on it
├── docs/
├── go.mod / go.sum
└── Makefile / lefthook.yml
```

Banned folder/file names at any level: `model.go`, `utils/`, `helpers/`,
`common/`, `ext/`, `adapter/`, `driver/`, `platform/`, `infra/`,
`kit/`, `repo/`. Name the concept, then name the file after it.

## Packages

- One word, lowercase, no underscores. Singular (`user`, `org`).
  Plural only for true collections.
- Interface (`store.go`, `service.go`) and implementation
  (`postgres.go`, `memory.go`) live in the same package.
- A cross-cutting package is promoted only when ≥3 domains import it
  or its own LoC exceeds 500. Never preemptively create
  `internal/infra/`, `internal/common/`, etc.
- `cmd/<binary>/main.go` is the only place that constructs concrete
  types and passes them to interfaces.

## Naming

| Kind | Rule | Example |
|------|------|---------|
| Package | lowercase, one word | `authz`, `grn` |
| Type | MixedCaps | `RoleAssignment` |
| Function / method | MixedCaps, verb+noun | `AssignRole` |
| Constant | MixedCaps | `DecisionAllow` |
| Acronym | uniform case | `HTTPClient`, `GRN`, `ID` |
| Receiver | short, consistent | `s *Service` |

## Imports

Three groups separated by blank lines: stdlib, third-party, internal.

```go
import (
    "context"

    "github.com/open-policy-agent/opa/v1/rego"

    "github.com/garamsh/gaon/internal/errorx"
    "github.com/garamsh/gaon/internal/grn"
)
```

Run `goimports -w .` before committing.

## Verification

| Task | Command |
|------|---------|
| Build | `go build ./...` |
| Test | `go test ./...` |
| Single test | `go test -run TestName ./pkg/...` |
| Vet | `go vet ./...` |
| Format check | `gofmt -l .` |
| Format apply | `gofmt -w .` |
| Import sort | `goimports -w .` |
| Module tidy | `go mod tidy` |
| Generate mocks | `mockery` |

CI and local run the same commands. See [ci.md](./ci.md).

## Dependencies

`internal/<domain>/<domain>.go` owns types. `service.go` owns the
contract. `<impl>.go` implements it. `mocks/` is generated.

Watch for circular imports at compile time and logical `type → struct →
type` cycles at review time — the compiler does not catch those.