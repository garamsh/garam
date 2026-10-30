import { APP_PARAM, type HandoffApp } from "@garam/platform-sdk-ts/handoff";
import { withReturnPath } from "@garam/platform-sdk-ts/return-path";

import { env } from "@/env";

/**
 * Where each app the site hands a sign-in to is served (ADR-0135). A
 * request names an app by its identifier, and this is the only place
 * the identifier becomes an origin, so no request decides where a code
 * or a signed-in visitor is sent.
 */
const ORIGINS: Record<HandoffApp, () => string> = {
  console: () => env.GARAM_CONSOLE_URL,
  cowork: () => env.GARAM_COWORK_URL,
};

/**
 * `app`'s address for `path`, a path on that app that was already
 * checked as a return path. Each app is another origin (ADR-0131), so a
 * link or a redirect to it names that origin rather than a path on this
 * one.
 */
export function appUrl(app: HandoffApp, path: string): string {
  return new URL(path, ORIGINS[app]()).toString();
}

/**
 * The site's `base` carrying the return path and, for any app but the
 * console, the app it is a path on. The console's addresses name no
 * app, as they did before another app was handed to.
 */
export function withApp(
  base: string,
  app: HandoffApp,
  path: string | null,
): string {
  const url = new URL(withReturnPath(base, path), "http://site.invalid");
  if (app !== "console") url.searchParams.set(APP_PARAM, app);
  return url.pathname + url.search;
}
