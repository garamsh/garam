/**
 * The identifier a GRN ends in, the last id of its path, for a name to
 * show where no display name is known
 * (`grn:<org>/<service>/<type>/<id>[#<locator>]`, ADR-0142). A string
 * that is not a GRN comes back as it is. This is the app's only reading
 * of the format.
 */
export function identifierOf(grn: string): string {
  if (!grn.startsWith("grn:")) return grn;
  const name = grn.split("#", 1)[0] as string;
  return name.split("/").at(-1) || grn;
}
