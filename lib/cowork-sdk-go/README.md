# lib/cowork-sdk-go

The Go client of cowork's HTTP API, with the types it answers,
generated from `cowork/api/openapi.yaml` by oapi-codegen (ADR-0133).
Nothing in it is written by hand.

- **Imported by** `garam-cli/`, whose `garam cowork` commands reach
  cowork through it ([cli.md](../../docs/architecture/cli.md)).
- **Regenerate** with `make -C lib/cowork-sdk-go gen`, which reads
  `oapi-codegen.yaml` and writes `client.gen.go`. A change to
  `cowork/api/openapi.yaml` regenerates it in the same pull request;
  `make -C lib/cowork-sdk-go check-gen` fails on output that does not
  match the specification.
- **Gate:** `make -C lib/cowork-sdk-go ci`.
