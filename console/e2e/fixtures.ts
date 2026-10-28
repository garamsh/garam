import { test as base, type BrowserContext } from "@playwright/test";

import { seedSignedInOrg, type Seed } from "./seed";

/** Where the site is served, beside the console this suite drives. */
export const SITE_URL =
  process.env.GARAM_E2E_SITE_URL ?? "http://garam.localhost:3001";

/**
 * The parent both hosts share, which the api scopes the session cookie
 * to (GARAM_COOKIE_DOMAIN, ADR-0131): the longest suffix the two
 * hostnames have in common.
 */
export function cookieDomain(consoleURL: string): string {
  const consoleHost = new URL(consoleURL).hostname.split(".").reverse();
  const site = new URL(SITE_URL).hostname.split(".").reverse();
  const shared: string[] = [];
  for (const [i, label] of consoleHost.entries()) {
    if (site[i] !== label) break;
    shared.push(label);
  }
  return shared.reverse().join(".");
}

/**
 * Plants `session` the way the api scopes it: on the parent the console
 * and the site share, so the site reads it as the console does.
 */
export async function holdSession(
  context: BrowserContext,
  consoleURL: string | undefined,
  session: string,
): Promise<void> {
  await context.addCookies([
    {
      name: "garam_session",
      value: session,
      domain: cookieDomain(consoleURL ?? "http://console.garam.localhost:3000"),
      path: "/",
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
}

/**
 * `signedIn` gives a test a freshly seeded organization and a browser
 * context already carrying its session cookie. Each test seeds its own
 * organization, so specs never contend over shared rows.
 */
export const test = base.extend<{ signedIn: Seed }>({
  signedIn: async ({ context, baseURL }, provide) => {
    const seed = await seedSignedInOrg();
    await holdSession(context, baseURL, seed.sessionToken);
    await provide(seed);
  },
});

export { expect } from "@playwright/test";
