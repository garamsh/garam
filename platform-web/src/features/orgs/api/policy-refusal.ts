import { GaramApiError, refusal } from "@/lib/garam";

/** What every policy write says of the policy service not answering. */
export const POLICY_SERVICE_TIMED_OUT =
  "The policy service did not answer in time. Try again in a moment.";

/** What activating or moving the default says of the default moving. */
export const DEFAULT_MOVED =
  "The policy's default version moved since this page was read. Reload the page and try again.";

/** What deleting says of the policy service holding it back. */
export const DELETE_HELD_BACK =
  "The policy service won't delete this policy as it stands. Reload the page and try again.";

/**
 * What a Server Action of the policy lifecycle returns for an error.
 * The policy service answers 409 when what the caller saw is no longer
 * so, and 504 when it did not answer in time; `refusal-copy.ts` has no
 * sentence for either, so the console words them for the lifecycle
 * rather than quoting garam. A 503 is the SDK's outage, which
 * `refusal` words; every other refusal is `refusal`'s too.
 */
export function policyRefusal(
  error: unknown,
  conflict: string,
): { status: "error"; message: string } {
  if (error instanceof GaramApiError) {
    if (error.status === 409) return { status: "error", message: conflict };
    if (error.status === 504) {
      return { status: "error", message: POLICY_SERVICE_TIMED_OUT };
    }
  }
  return refusal(error);
}

/** What is written: a guardrail or permit's Cedar, a link, a version. */
export type PolicyWrite = "create-cedar" | "create-link" | "version";

/**
 * What a write of a policy or a version returns for an error. `at`
 * says where the form shows it: the name, the body (the Cedar editor
 * or the role form's fields), or beside the submit.
 */
export type PolicyWriteError = {
  status: "error";
  at: "name" | "body" | "form";
  message: string;
};

/** What each write says of the policy service refusing the caller. */
const WRITE_FORBIDDEN: Record<PolicyWrite, string> = {
  "create-cedar": "You can't create this kind of policy in this organization.",
  "create-link": "You can't write a role link in this organization.",
  version: "You can't add a version to this policy.",
};

/** What each write says of the policy service holding what it clashes with. */
const WRITE_CONFLICT: Record<PolicyWrite, string> = {
  "create-cedar":
    "The policy service holds a policy that conflicts with this one.",
  "create-link": "The policy service already holds this role link.",
  version:
    "The policy changed since this page was read. Reload the page and try again.",
};

/**
 * What a Server Action that writes a policy returns for an error. A
 * 400 carries the policy service's own message, shown at the body it
 * refused, because the service parses Cedar and knows what is wrong
 * with it. 403, 404, 409 and 504 are worded for the write, which
 * `refusal-copy.ts` does not do for these operations; every other
 * error is `refusal`'s.
 */
export function policyWriteRefusal(
  error: unknown,
  write: PolicyWrite,
): PolicyWriteError {
  if (error instanceof GaramApiError) {
    switch (error.status) {
      case 400:
        return { status: "error", at: "body", message: error.message };
      case 403:
        return { status: "error", at: "form", message: WRITE_FORBIDDEN[write] };
      case 404:
        return {
          status: "error",
          at: "form",
          message: "This policy is no longer in the organization.",
        };
      case 409:
        return { status: "error", at: "form", message: WRITE_CONFLICT[write] };
      case 504:
        return {
          status: "error",
          at: "form",
          message: POLICY_SERVICE_TIMED_OUT,
        };
    }
  }
  return { at: "form", ...refusal(error) };
}
