/**
 * The parts a GRN carries:
 * `grn:<organization>/<service>/<type>/<id>[/<type>/<id>...][#<locator>]`
 * (`docs/architecture/grn-model.md` §Format). `serviceType` and
 * `identifier` are the resource's own, the last type and id pair;
 * `ancestors` holds the pairs before it as `type/id[/type/id...]`, empty
 * when there are none.
 */
export type Grn = {
  organization: string;
  service: string;
  ancestors: string;
  serviceType: string;
  identifier: string;
  locator: string | null;
};

/** The longest name before `#`, `grn:` included (ADR-0142). */
const NAME_MAX = 512;

/** One segment: lowercase letters, digits and `-`, at most 64. */
const SEGMENT = /^[a-z0-9-]{1,64}$/;

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
 * rule. The five-segment form and a name past 512 characters before
 * `#` spell none.
 */
export function parseGrn(value: string): Grn | null {
  if (!value.startsWith("grn:")) {
    return null;
  }
  const hash = value.indexOf("#");
  const name = hash === -1 ? value : value.slice(0, hash);
  const locator = hash === -1 ? null : value.slice(hash + 1);
  if (name.length > NAME_MAX || locator === "") {
    return null;
  }
  const segments = name.slice("grn:".length).split("/");
  const [organization, service, ...path] = segments;
  if (
    organization === undefined ||
    service === undefined ||
    path.length < 2 ||
    path.length % 2 !== 0 ||
    !segments.every((segment) => SEGMENT.test(segment))
  ) {
    return null;
  }
  return {
    organization,
    service,
    ancestors: path.slice(0, -2).join("/"),
    serviceType: path[path.length - 2] as string,
    identifier: path[path.length - 1] as string,
    locator,
  };
}

/**
 * The Resource pattern that matches every project of an organization:
 * the one string the console builds from pieces, so no page spells the
 * format.
 */
export function projectsPattern(organization: string): string {
  return `grn:${organization}/iam/project/*`;
}

/**
 * A GRN written from its parts, for a fixture that stands in for a name
 * the platform minted.
 */
export function formatGrn(
  organization: string,
  service: string,
  serviceType: string,
  identifier: string,
): string {
  return `grn:${organization}/${service}/${serviceType}/${identifier}`;
}
