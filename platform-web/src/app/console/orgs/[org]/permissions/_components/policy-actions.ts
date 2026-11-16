import type { IamPolicyKind } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

/** The kinds in the order the create form offers them. */
export const CREATE_KINDS: readonly IamPolicyKind[] = [
  "guardrail",
  "permit",
  "role-link",
];

/**
 * The action that creates each kind, asked on the organization: a
 * policy that does not exist yet has no other resource to be asked on.
 */
export const CREATE_ACTION: Record<IamPolicyKind, string> = {
  guardrail: "iam:policy.create",
  permit: "iam:permit.write",
  "role-link": "iam:policy.attach",
};

/** The kinds the caller is not known to be denied creating. */
export async function creatableKinds(org: string) {
  const permissions = await checkPermissions(
    org,
    Object.fromEntries(
      CREATE_KINDS.map((kind) => [kind, { action: CREATE_ACTION[kind] }]),
    ),
  );
  return CREATE_KINDS.filter((kind) => permissions.get(kind) !== "denied");
}

/**
 * The action a write on each kind is taken as, asked on the
 * organization, never on the policy: `write` for activating a policy,
 * moving its default version or adding a version, `remove` for
 * deleting it. The policy service decides each write there, and the
 * platform answers `iam:permit.write` nowhere else. It takes a
 * permit's removal as it takes a guardrail's, `iam:policy.delete`, and
 * every other write of one as `iam:permit.write`.
 */
export const WRITE_ACTION: Record<
  IamPolicyKind,
  { write: string; remove: string }
> = {
  "role-link": { write: "iam:policy.attach", remove: "iam:policy.detach" },
  guardrail: { write: "iam:policy.update", remove: "iam:policy.delete" },
  permit: { write: "iam:permit.write", remove: "iam:policy.delete" },
};
