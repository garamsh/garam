# lib/clisession

A person's CLI session with garam's platform (ADR-0120, ADR-0132): the
device grant that signs a CLI in, the refresh that keeps its session
live, and the credentials file it is kept in between runs. The package
doc in `store.go` is authoritative for the file and the grant.

- **Imported by** `cowork/`, whose CLI signs in through it.
  `platform-cli/` keeps its own code for the same credentials file
  until #1680 moves it onto this module.
- **Gate:** `make -C lib/clisession ci`.
