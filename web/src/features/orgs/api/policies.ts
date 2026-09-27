import { z } from "zod";

import { garamFetchAuthenticated, parserFor } from "@/lib/garam";
import type { PolicyDocument } from "@/lib/policy-document";
import type { components } from "@/types/api";

import { parseGrn } from "../grn";
import { orgPath, refusableRead, type RefusableRead } from "./orgs";

/**
 * A policy of an organization, as the API returns one. Only an
 * `active` one contributes statements to a check, and then its
 * `defaultVersion` alone (`docs/architecture/authz.md`).
 */
export type Policy = components["schemas"]["Policy"];
/** A policy's lifecycle state. */
export type PolicyStatus = components["schemas"]["PolicyStatus"];
/**
 * One stored version of a policy. `document` is typed as the console
 * holds it rather than as generated (`PolicyDocument`).
 */
export type PolicyVersion = Omit<
  components["schemas"]["PolicyVersion"],
  "document"
> & { document: PolicyDocument };
/**
 * One subject a policy is attached to. `subjectGrn` may name a user,
 * a group or an agent, and nothing guarantees it still names any of
 * them — resolving it is the reader's job.
 */
export type PolicyAttachment = components["schemas"]["PolicyAttachment"];

const GrnParser = z.string().refine((value) => parseGrn(value) !== null);

/** Parses a policy as `api/openapi.yaml` describes one. */
export const PolicyParser = parserFor<Policy>()(
  z.object({
    grn: GrnParser,
    orgGrn: GrnParser,
    name: z.string(),
    status: z.enum(["draft", "active", "archived"]),
    defaultVersion: z.number().int().min(1),
    createdAt: z.string(),
    updatedAt: z.string(),
  }),
);

/**
 * Parses a policy version: the generated fields, and the document as a
 * JSON object rather than the generated `Record<string, never>`.
 */
export const PolicyVersionParser = parserFor<
  Omit<components["schemas"]["PolicyVersion"], "document">
>()(
  z.object({
    policyGrn: GrnParser,
    version: z.number().int().min(1),
    createdBy: GrnParser,
    createdAt: z.string(),
  }),
).and(z.object({ document: z.record(z.string(), z.unknown()) }));

/** Parses one attachment of a policy to a subject. */
export const PolicyAttachmentParser = parserFor<PolicyAttachment>()(
  z.object({
    policyGrn: GrnParser,
    subjectGrn: GrnParser,
    createdAt: z.string(),
  }),
);

/**
 * An organization's policies. Reading them is an admin action, so a
 * refusal is a state of its own; a page reading this also reads the
 * organization, whose refusal stays not-found (ADR-0011).
 */
export function listPolicies(org: string): Promise<RefusableRead<Policy[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/policies`,
      z.array(PolicyParser),
    ),
  );
}

/**
 * The stored versions of one policy, oldest first. garam answers an
 * empty list for a policy that does not exist, so a page establishes
 * that the policy exists from `listPolicies`.
 */
export function listPolicyVersions(
  org: string,
  policy: string,
): Promise<RefusableRead<PolicyVersion[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/policies/${orgPath(policy)}/versions`,
      z.array(PolicyVersionParser),
    ),
  );
}

/**
 * The subjects one policy is attached to. As with versions, a policy
 * that does not exist answers an empty list.
 */
export function listPolicyAttachments(
  org: string,
  policy: string,
): Promise<RefusableRead<PolicyAttachment[]>> {
  return refusableRead(
    garamFetchAuthenticated(
      `/orgs/${orgPath(org)}/policies/${orgPath(policy)}/attachments`,
      z.array(PolicyAttachmentParser),
    ),
  );
}
