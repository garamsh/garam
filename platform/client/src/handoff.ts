/**
 * The names and the state a sign-in handoff runs on (ADR-0131). The
 * session cookie is host-only, so a person signs in on the site and an
 * app on another host is handed a session of its own: the app sends the
 * visitor to the site's `HANDOFF_PATH` with a state it holds in a cookie
 * of its own, the site answers with a one-time code on the app's
 * `HANDOFF_REDEEM_PATH`, and the app's server redeems the code with that
 * state. The state is what makes a code redeem only in the browser that
 * asked for it: a code an attacker obtained meets a state the victim's
 * browser never held.
 */

/** The site's route an app sends a visitor to for a session. */
export const HANDOFF_PATH = "/handoff";

/** The app's route the site sends the visitor back to with a code. */
export const HANDOFF_REDEEM_PATH = "/auth/handoff";

/** The app's route that draws a state and sends the visitor to the site. */
export const HANDOFF_START_PATH = "/auth/sign-in";

/** The cookie the app holds its state in, read back only on redemption. */
export const HANDOFF_STATE_COOKIE = "garam_handoff_state";

/** How long a handoff may take, from the app's redirect to its redemption. */
export const HANDOFF_STATE_MAX_AGE = 10 * 60;

export const STATE_PARAM = "state";
export const CODE_PARAM = "code";

/** A fresh state: 32 random bytes, base64url, 43 characters. */
export function newHandoffState(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  return btoa(String.fromCharCode(...bytes))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}

/**
 * `value` as a state, or null where it is not one: base64url within the
 * bounds `POST /auth/handoff` accepts, so a value the backend would
 * refuse is refused before it is sent on.
 */
export function handoffStateOf(value: unknown): string | null {
  return typeof value === "string" && /^[A-Za-z0-9_-]{32,128}$/.test(value)
    ? value
    : null;
}

/**
 * `value` as a code, or null where it is not one: the base64url token
 * the backend draws, within the bound it reads.
 */
export function handoffCodeOf(value: unknown): string | null {
  return typeof value === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(value)
    ? value
    : null;
}
