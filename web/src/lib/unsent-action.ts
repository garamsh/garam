import { unstable_rethrow } from "next/navigation";

/**
 * What a dialog says when garam's answer never arrived (#1631). The
 * write may have landed before a proxy gave up, so it claims neither.
 */
export const UNSENT_COPY =
  "garam didn't answer, so this may or may not have been saved. Check before trying again.";

/**
 * Whether a Server Action call ended with no answer from garam: the
 * action's own request was answered by something else, as a 503 from a
 * proxy in front of the console is, or its connection dropped. garam
 * may still have committed the write. Next.js rejects such a call with
 * no result to render. Its own navigation (a redirect, a not-found) is
 * rethrown by `unstable_rethrow`, and an error the action threw on the
 * server carries a `digest`; both stay throws, the second for
 * `error.tsx` (`docs/convention/stack-nextjs.md` §7).
 */
export function isUnsent(error: unknown): boolean {
  unstable_rethrow(error);
  return !(error instanceof Error && "digest" in error);
}

/**
 * `action`, answering a call garam's answer never came back from as a
 * refusal the dialog renders, instead of a rejection that unmounts it.
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
