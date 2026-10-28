# lib/grn

Garam Resource Names, the one identifier for every resource in garam
(ADR-0132). `docs/architecture/grn-model.md` describes the format, and
the doc comments in `grn.go` are authoritative for constructing,
rendering, parsing and validating one.

- **Imported by** `platform/` and `cowork/`.
- **Gate:** `make -C lib/grn ci`.
