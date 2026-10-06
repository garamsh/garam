/**
 * The segments a GRN carries:
 * `grn:<organization>:<project>:<service_type>:<identifier>`
 * (`docs/architecture/grn-model.md` §Format).
 */
export type Grn = {
  organization: string;
  project: string;
  serviceType: string;
  identifier: string;
};

/**
 * The identifier segment of a GRN. API paths address organizations,
 * groups, projects and policies by that last segment, so routes carry
 * the segment rather than the GRN. The seam refuses a GRN field
 * `parseGrn` rejects, so the throw guards an invariant rather than a
 * response body.
 */
export function slugFromGrn(grn: string): string {
  const parsed = parseGrn(grn);
  if (parsed === null) {
    throw new Error(`malformed GRN: ${grn}`);
  }
  return parsed.identifier;
}

/**
 * The GRN a string spells, or null when it spells none. This is the
 * console's only reading of the format: what the seam refuses and what
 * `slugFromGrn` returns are the same parse, not two spellings of one
 * rule.
 */
export function parseGrn(value: string): Grn | null {
  const [prefix, organization, project, serviceType, identifier, ...extra] =
    value.split(":");
  if (
    prefix !== "grn" ||
    organization === undefined ||
    project === undefined ||
    serviceType === undefined ||
    identifier === undefined ||
    extra.length > 0
  ) {
    return null;
  }
  return { organization, project, serviceType, identifier };
}
