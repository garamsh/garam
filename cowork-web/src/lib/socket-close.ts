import { HANDOFF_START_PATH } from "@garam/platform-sdk-ts/handoff";
import { withReturnPath } from "@garam/platform-sdk-ts/return-path";

/**
 * The close codes cowork's server ends a WebSocket with once the
 * platform refuses its caller (ADR-0139), each the status its upgrade
 * would now be answered. y-websocket does not reconnect after either.
 */
const SIGNED_OUT = 4401;
const NO_ACCESS = 4403;

/** Where a tab asks whether its session stands (`app/auth/session`). */
const SESSION_PATH = "/auth/session";

function signIn(): void {
  const { pathname, search } = window.location;
  window.location.assign(withReturnPath(HANDOFF_START_PATH, pathname + search));
}

/**
 * Leaves the page a closed socket was serving when the close means the
 * visitor may not stay. A visitor signed out goes to sign in by the
 * handoff, carrying the page, and one who may no longer see it
 * reloads, for the page to say so. Any other close may be a reconnect
 * into a refusal a browser cannot read, so it asks whether the session
 * stands and signs in only on a refusal: a network failure or a
 * platform error says nothing about the session.
 */
export async function leaveOnClose(code: number): Promise<void> {
  if (code === SIGNED_OUT) return signIn();
  if (code === NO_ACCESS) return window.location.reload();
  let response: Response;
  try {
    response = await fetch(new URL(SESSION_PATH, window.location.origin), {
      cache: "no-store",
    });
  } catch {
    // Unreachable: the socket's next close asks again.
    return;
  }
  if (response.status === 401) signIn();
}
