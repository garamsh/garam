# lib/platform-sdk-go

The Go client of the platform's browser API, with the types it answers,
generated from `platform/api/openapi.yaml` by oapi-codegen (ADR-0132).
Nothing in it is written by hand.

- **Imported by** `garam-cli/`, which reaches the platform through
  it alone ([cli.md](../../docs/architecture/cli.md)).
- **Regenerate** with `make -C lib/platform-sdk-go gen`, which reads
  `oapi-codegen.yaml` and writes `client.gen.go`. A change to
  `platform/api/openapi.yaml` regenerates it in the same pull request;
  `make -C lib/platform-sdk-go check-gen` fails on output that does not
  match the specification.
- **Gate:** `make -C lib/platform-sdk-go ci`.
