/**
 * A GRN is `grn:<organization>:<project>:<service_type>:<identifier>`.
 * API paths address organizations, groups, projects and policies by
 * that last segment, so routes carry the segment rather than the GRN.
 */
export function slugFromGrn(grn: string): string {
  const segments = grn.split(":");
  if (segments.length !== 5 || segments[0] !== "grn") {
    throw new Error(`malformed GRN: ${grn}`);
  }
  return segments[4];
}
