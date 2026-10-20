import { unstable_rethrow } from "next/navigation";

/** What a dialog says when its request never reached garam (#1631). */
export const UNSENT_COPY =
  "Nothing was saved: the request didn't go through. Try again.";

/**
 * Whether a Server Action call failed before garam answered: the
 * action's own request was refused or never arrived, as a 503 from a
 * proxy in front of the console or a dropped connection does. Next.js
 * rejects such a call with no result to render. Its own navigation (a
 * redirect, a not-found) is rethrown by `unstable_rethrow`, and an error
 * the action threw on the server carries a `digest`; both stay throws,
 * the second for `error.tsx` (`docs/convention/stack-nextjs.md` §7).
 */
export function isUnsent(error: unknown): boolean {
  unstable_rethrow(error);
  return !(error instanceof Error && "digest" in error);
}

/**
 * `action`, answering a request that never reached garam as a refusal
 * the dialog renders, instead of a rejection that unmounts it.
 */
export function refuseUnsent<S, P>(
  action: (state: S, payload: P) => Promise<S>,
  message: string = UNSENT_COPY,
): (state: S, payload: P) => Promise<S | { status: "error"; message: string }> {
  return async (state, payload) => {
    try {
      return await action(state, payload);
    } catch (error) {
      if (!isUnsent(error)) throw error;
      return { status: "error", message };
    }
  };
}
