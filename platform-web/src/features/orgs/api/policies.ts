import { z } from "zod";

import {
  GaramApiError,
  GaramUnreachableError,
  garamFetchAuthenticated,
  parserFor,
} from "@/lib/garam";
import type { components } from "@garam/platform-sdk-ts/api";

import { orgPath, refusableRead, type RefusableRead } from "./orgs";

/**
 * A policy of an organization as the policy service holds it, read
 * through garam's relay (ADR-0169). Only an `active` one decides, and
 * then its `defaultVersion` alone.
 */
export type IamPolicy = components["schemas"]["IamPolicy"];
/** What a policy is: a role link, a guardrail or a permit. */
export type IamPolicyKind = components["schemas"]["IamPolicyKind"];
/** A policy's lifecycle state. */
export type PolicyStatus = components["schemas"]["PolicyStatus"];
/** What a role link says: who holds which role over what. */
export type IamRoleLink = components["schemas"]["IamRoleLink"];
/**
 * What one version holds: a role link's facts, or the Cedar text of a
 * guardrail or a permit. The specification leaves both optional beside
 * the kind; the policy service sends the one the kind names, and a
 * body that does not is refused at the seam.
 */
export type IamPolicyBody =
  | { kind: "role-link"; roleLink: IamRoleLink }
  | { kind: "guardrail" | "permit"; text: string };
/** A policy with every version it holds, oldest first. */
export type IamPolicyDetail = Omit<
  components["schemas"]["IamPolicyDetail"],
  "versions"
> & {
  versions: (Omit<
    components["schemas"]["IamPolicyDetail"]["versions"][number],
    "body"
  > & { body: IamPolicyBody })[];
};

const KindParser = parserFor<IamPolicyKind>()(
  z.enum(["role-link", "guardrail", "permit"]),
);
const StatusParser = parserFor<PolicyStatus>()(z.enum(["draft", "active"]));

/** Parses a policy as `platform/api/openapi.yaml` describes one. */
const IamPolicyParser = parserFor<IamPolicy>()(
  z.object({
    id: z.string().min(1),
    name: z.string(),
    kind: KindParser,
    status: StatusParser,
    defaultVersion: z.number().int().min(1),
  }),
);

const IamRoleLinkParser = parserFor<IamRoleLink>()(
  z.object({
    role: z.enum(["owner", "admin", "member"]),
    scope: z.enum(["organization", "team-or-project"]),
    principal: z.string(),
    resource: z.string(),
  }),
);

// Narrower than the specification on purpose (see `IamPolicyBody`), so
// it is typed by what it produces rather than by `parserFor`.
const IamPolicyBodyParser: z.ZodType<IamPolicyBody> = z.discriminatedUnion(
  "kind",
  [
    z.object({ kind: z.literal("role-link"), roleLink: IamRoleLinkParser }),
    z.object({ kind: z.enum(["guardrail", "permit"]), text: z.string() }),
  ],
);

const IamPolicyDetailParser: z.ZodType<IamPolicyDetail> = z.object({
  policy: IamPolicyParser,
  versions: z.array(
    z.object({
      version: z.number().int().min(1),
      body: IamPolicyBodyParser,
      createdBy: z.string(),
    }),
  ),
});

/**
 * A read of the policy service. It answers 504 when it did not answer
 * in time, which is an outage the page says so and not a refusal, so
 * it is read as the service being unreachable.
 */
async function viaPolicyService<T>(read: Promise<T>): Promise<T> {
  try {
    return await read;
  } catch (error) {
    if (error instanceof GaramApiError && error.status === 504) {
      throw new GaramUnreachableError({ cause: error });
    }
    throw error;
  }
}

/**
 * An organization's policies. Reading them is the policy service's
 * decision, so a refusal is a state of its own; a page reading this
 * also reads the organization, whose refusal stays not-found
 * (ADR-0011).
 */
export function listIamPolicies(
  org: string,
): Promise<RefusableRead<IamPolicy[]>> {
  return refusableRead(
    viaPolicyService(
      garamFetchAuthenticated(
        `/orgs/${orgPath(org)}/iam-policies`,
        z.array(IamPolicyParser),
      ),
    ),
  );
}

/**
 * One policy with its versions. A policy the organization does not
 * hold is a 404, which `refusableRead` leaves to throw.
 */
export function getIamPolicy(
  org: string,
  policy: string,
): Promise<RefusableRead<IamPolicyDetail>> {
  return refusableRead(
    viaPolicyService(
      garamFetchAuthenticated(
        `/orgs/${orgPath(org)}/iam-policies/${orgPath(policy)}`,
        IamPolicyDetailParser,
      ),
    ),
  );
}
