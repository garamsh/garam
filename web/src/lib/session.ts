/**
 * Name of the session cookie the backend issues on OAuth callback.
 * It lives apart from `garam.ts` so `proxy.ts`, which runs before the
 * Node server modules load, can read it.
 */
export const SESSION_COOKIE = "garam_session";
