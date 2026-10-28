# lib/errorx

The typed errors every Go module of this repository returns, each
carrying a kind aligned with a gRPC status code (ADR-0003, ADR-0132).
The package doc in `errorx.go` is authoritative for the kinds and the
calls.

- **Imported by** `platform/`, `garam-cli/`, `cowork/` and
  `lib/grn/`.
- **Gate:** `make -C lib/errorx ci`.
