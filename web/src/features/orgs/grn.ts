/**
 * A GRN is `grn:<organization>:<project>:<service_type>:<identifier>`.
 * API paths address organizations, groups, projects and policies by
 * that last segment, so routes carry the segment rather than the GRN.
 */
export function slugFromGrn(grn: string): string {
  if (!isGrn(grn)) {
    throw new Error(`malformed GRN: ${grn}`);
  }
  return grn.split(":")[4];
}

/**
 * Whether `slugFromGrn` can read the string. The seam refuses a GRN
 * field this rejects, so the throw above guards an invariant rather
 * than a response body.
 */
export function isGrn(value: string): boolean {
  const segments = value.split(":");
  return segments.length === 5 && segments[0] === "grn";
}

/**
 * The GRN of an operator of `org`, built from the identifier the
 * console addresses it by. An operator is org-level, so its project
 * segment is `default` (grn-model.md §Ownership). `identifier` is what
 * `parseOperatorIdentifier` returned, so it carries no colon of its own.
 */
export function operatorGrn(org: string, identifier: string): string {
  return `grn:${org}:default:operator:${identifier}`;
}
