import { z } from "zod";

import type { components } from "@garam/platform-sdk-ts/api";

/**
 * What the policy forms submit as a body, read as a union on `kind`:
 * the fields a role link carries, or the Cedar text of a guardrail or a
 * permit. The service validates what it is sent again, and a pair it
 * refuses comes back as a 400 the form shows, so only the shape is
 * checked here.
 */
export const PolicyBodyForm = z.discriminatedUnion("kind", [
  z.object({
    kind: z.literal("role-link"),
    role: z.enum(["owner", "admin", "member"]),
    scope: z.enum(["organization", "team-or-project"]),
    principal: z.string().min(1),
    resource: z.string().min(1),
  }),
  z.object({
    kind: z.enum(["guardrail", "permit"]),
    text: z.string().refine((text) => text.trim() !== ""),
  }),
]);

/** A body as the form submitted it, parsed. */
export type PolicyBodyFields = z.infer<typeof PolicyBodyForm>;

/** What the form's body fields say when they are not a body. */
export const BODY_INCOMPLETE = "Complete the policy's body.";

/** The body the wire carries for the fields a form submitted. */
export function wireBody(
  fields: PolicyBodyFields,
): components["schemas"]["IamPolicyBody"] {
  if (fields.kind === "role-link") {
    const { role, scope, principal, resource } = fields;
    return {
      kind: "role-link",
      roleLink: { role, scope, principal, resource },
    };
  }
  return { kind: fields.kind, text: fields.text };
}
