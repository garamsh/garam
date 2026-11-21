import type { IamPolicyKind } from "@/features/orgs";

/** What a policy is, as the word the console shows. */
export const POLICY_KIND_LABEL: Record<IamPolicyKind, string> = {
  "role-link": "Role link",
  guardrail: "Guardrail",
  permit: "Permit",
};
