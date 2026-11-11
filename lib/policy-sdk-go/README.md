# lib/policy-sdk-go

The Go client of the policy service's gRPC API, with the messages it
exchanges, generated from `policy/proto/garam/policy/v1/policy.proto`
by buf (ADR-0130). Nothing in it is written by hand.

- **Imported by** `platform/`, which feeds the service through
  `ApplyEntityChanges` ([authz.md](../../docs/architecture/authz.md),
  ADR-0140).
- **Regenerate** with `make -C lib/policy-sdk-go gen`, which reads
  `buf.gen.yaml` and writes `gen/`. A change to the policy service's
  proto regenerates it in the same pull request;
  `make -C lib/policy-sdk-go check-gen` fails on output that does not
  match the proto.
- **Gate:** `make -C lib/policy-sdk-go ci`.
