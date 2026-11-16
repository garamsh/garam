import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
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
/** What one version holds: Cedar text, or a role link. */
export type IamPolicyBody = components["schemas"]["IamPolicyBody"];
/** One stored version of a policy. */
export type IamPolicyVersion = components["schemas"]["IamPolicyVersion"];
/** A policy with every version it holds, oldest first. */
export type IamPolicyDetail = components["schemas"]["IamPolicyDetail"];

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

const IamPolicyBodyParser = parserFor<IamPolicyBody>()(
  z.object({
    kind: KindParser,
    text: z.string().optional(),
    roleLink: IamRoleLinkParser.optional(),
  }),
);

const IamPolicyDetailParser = parserFor<IamPolicyDetail>()(
  z.object({
    policy: IamPolicyParser,
    versions: z.array(
      z.object({
        version: z.number().int().min(1),
        body: IamPolicyBodyParser,
        createdBy: z.string(),
      }),
    ),
  }),
);

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
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/iam-policies`,
      z.array(IamPolicyParser),
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
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/iam-policies/${orgPath(policy)}`,
      IamPolicyDetailParser,
    ),
  );
}
