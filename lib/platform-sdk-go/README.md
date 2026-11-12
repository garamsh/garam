# lib/platform-sdk-go

The Go client of the platform's browser API, with the types it answers,
generated from `platform/api/openapi.yaml` by oapi-codegen (ADR-0132).
Nothing in it is written by hand.

It holds a second client, the package `machine`
(`github.com/garamsh/garam/lib/platform-sdk-go/machine`), generated
from `platform/api/machine.yaml`, the machine listener's specification,
for a caller of that listener: `exchangeClientAssertion`,
`mintAgentToken`, `reportProvisioningState` and the rest of its
operations. It is a package of its own because both specifications name
operations and schemas alike. Its test is `machine/client_test.go`.

- **Imported by** `garam-cli/`, which reaches the platform through
  it alone ([cli.md](../../docs/architecture/cli.md)).
- **Regenerate** with `make -C lib/platform-sdk-go gen`, which reads
  `oapi-codegen.yaml` and `machine-codegen.yaml` and writes
  `client.gen.go` and `machine/client.gen.go`. A change to
  `platform/api/openapi.yaml` or `platform/api/machine.yaml`
  regenerates them in the same pull request;
  `make -C lib/platform-sdk-go check-gen` fails on output that does not
  match the specification.
- **Gate:** `make -C lib/platform-sdk-go ci`.
