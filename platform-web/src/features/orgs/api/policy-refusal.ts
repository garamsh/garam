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
