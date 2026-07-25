# ADR 0001: GRN organization/project segments identify owning resources

> Status: accepted
> Date: 2026-07-25

## Context

The GRN format `grn:<organization>:<project>:<service_type>:<identifier>`
originally fixed the organization segment to the literal `garam` as a
company marker, with multitenancy deferred as a future decision. Two
problems: the `grn` prefix already names the vendor (Garam Resource
Name), making the literal redundant; and fixing the segment would have
forced a migration of every stored GRN when multitenancy arrives. The
domain model already treats `Organization` as the top of the tenant
hierarchy.

## Decision

The organization and project segments carry the identifiers of the
owning resources (e.g. `grn:acme:frontend:user:alice`). No segment is
fixed. The reserved slug `root` owns root-level resources —
organizations themselves (`grn:root:default:org:acme`) — and may not
be used as a real organization identifier. `default` marks
org-level (project-less) ownership.

## Consequences

- Multitenancy is structural from the first stored GRN; no future
  identifier migration.
- authz can scope checks by organization/project segments.
- Resource creation requires knowing the owning org and project.
- The stale "fixed to garam" wording in `internal/grn/grn.go` doc
  comments must be updated by the next PR touching the package.
