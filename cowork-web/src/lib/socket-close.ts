import { HANDOFF_START_PATH } from "@garam/platform-sdk-ts/handoff";
import { withReturnPath } from "@garam/platform-sdk-ts/return-path";

/**
 * The close codes cowork's server ends a WebSocket with once the
 * platform refuses its caller (ADR-0139), each the status its upgrade
 * would now be answered. y-websocket does not reconnect after either.
 */
const SIGNED_OUT = 4401;
const NO_ACCESS = 4403;

/**
 * Leaves the page a refused socket was serving: a visitor signed out
 * goes to sign in by the handoff, carrying the page, and one who may
 * no longer see it reloads, for the page to say so. Any other close is
 * the socket's own to recover from.
 */
export function leaveOnRefusal(code: number): void {
  const { pathname, search } = window.location;
  if (code === SIGNED_OUT) {
    window.location.assign(
      withReturnPath(HANDOFF_START_PATH, pathname + search),
    );
  } else if (code === NO_ACCESS) {
    window.location.reload();
  }
}
