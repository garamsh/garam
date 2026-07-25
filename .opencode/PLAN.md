# Plan — garam core model (interfaces only)

## 1. Goal

Build the foundational model layer as **interfaces and types only**.
In-memory store implementations and tests that require them are
deferred to a follow-up plan.

- `internal/grn` — value-type `GRN`.
- `internal/errorx` — mdrive-style typed errors.
- `internal/org` — `Organization`, `Group`, `Project` types +
  `Store` interface.
- `internal/user`, `internal/agent` — types + `Store` interface.
- `internal/authz` — `Subject`/`Object`, `Role`, `Policy`,
  `Manager`, `Checker` types and interfaces.

## 2. Scope

**In scope:**
- All packages above with types + Store/Repository/Manager interfaces.
- `errorx` + `grn` packages keep their tests (pure logic, no Store).
- `go build ./...`, `go vet ./...`, `gofmt -l .` clean.

**Out of scope (deferred to next plan):**
- All `memory.go` in-memory store implementations.
- Domain test files that require Store implementations.
- Mockery mock generation (requires Store impls to exercise).
- Rego/OPA integration.
- Doc/Repo domains.

## 3. Constraints

- `go-conventions.md`: domain-centric packages; `store.go` owns the
  Store interface; `memory.go` and tests deferred.
- `code-comment-rules.md`: short doc comments on exported names only.

## 4. Implementation

### 4.1 Packages

#### `internal/grn`
Same as before: `GRN` value type + `Parse`, `New`, `Validate`, `String`,
`IsZero`. Tests stay.

#### `internal/errorx`
mdrive-style typed errors. Tests stay.

#### `internal/org`
- `Organization`, `Group`, `Project` types.
- `Store` interface (CRUD + membership + cascade delete via
  `DeleteOrg`).
- `Patch`, `ListFilter` types.
- **No `memory.go`, no test file yet.**

#### `internal/user`, `internal/agent`
- `User` and `Agent` types.
- `Store` interface (CRUD + organization membership management).
- **No `memory.go`, no test file yet.**

#### `internal/authz`
- `Subject` and `Object` interfaces.
- `Role`, `Policy`, `RoleStore`, `PolicyStore`.
- `Manager` interface (Grant/Revoke/Explain/ListChanges) +
  `Change` event type.
- `Checker` (Allow).
- **No in-memory implementation, no test file yet.**

## 5. Files Touched

| File | Action |
|------|--------|
| `internal/grn/grn.go` | Create |
| `internal/grn/grn_test.go` | Create |
| `internal/errorx/errorx.go` | Create |
| `internal/errorx/errorx_test.go` | Create |
| `internal/org/org.go` | Create |
| `internal/org/group.go` | Create |
| `internal/org/project.go` | Create |
| `internal/org/store.go` | Create |
| `internal/user/user.go` | Create |
| `internal/user/store.go` | Create |
| `internal/agent/agent.go` | Create |
| `internal/agent/store.go` | Create |
| `internal/authz/subject.go` | Create |
| `internal/authz/object.go` | Create |
| `internal/authz/role.go` | Create |
| `internal/authz/policy.go` | Create |
| `internal/authz/manager.go` | Create |
| `internal/authz/check.go` | Create |

## 6. Architecture / Style Decisions

- Each domain owns its Store interface (no shared "Repository"
  abstraction).
- `Subject` and `Object` interfaces return `grn.GRN` value type.
- `Manager` operates on `grn.GRN` strings and is the only place where
  Grant/Revoke/Explain/ListChanges exist.

## 7. Verification

- `go build ./...`
- `go vet ./...`
- `go test ./...` (only grn, errorx have tests for now)
- `gofmt -l .` empty.

## 8. Risks & Open Questions

- In-memory implementations are deferred; once added, domain tests can
  be written.
- Mockery will run after memory.go lands.

## 9. Deviations Log

- **Step 4 (build execution)**: User asked to skip in-memory store
  implementations. `memory.go` and tests that require them removed.
  Follow-up plan will add them.
