import { test as base, type BrowserContext } from "@playwright/test";

import { seedSignedInOrg, type Seed } from "./seed";

/** Where the console is served, beside the site this suite drives. */
export const CONSOLE_URL =
  process.env.GARAM_E2E_CONSOLE_URL ?? "http://console.garam.localhost:3000";

/**
 * The parent both hosts share, which the api scopes the session cookie
 * to (GARAM_COOKIE_DOMAIN, ADR-0131): the longest suffix the two
 * hostnames have in common.
 */
export function cookieDomain(siteURL: string): string {
  const site = new URL(siteURL).hostname.split(".").reverse();
  const consoleHost = new URL(CONSOLE_URL).hostname.split(".").reverse();
  const shared: string[] = [];
  for (const [i, label] of site.entries()) {
    if (consoleHost[i] !== label) break;
    shared.push(label);
  }
  return shared.reverse().join(".");
}

/** Plants `session` the way the api scopes it: on the shared parent. */
export async function holdSession(
  context: BrowserContext,
  siteURL: string,
  session: string,
): Promise<void> {
  await context.addCookies([
    {
      name: "garam_session",
      value: session,
      domain: cookieDomain(siteURL),
      path: "/",
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
}

/**
 * `signedIn` gives a test a freshly seeded organization and a browser
 * context already carrying its owner's session cookie. Each test seeds
 * its own organization, so specs never contend over shared rows.
 */
export const test = base.extend<{ signedIn: Seed }>({
  signedIn: async ({ context, baseURL }, provide) => {
    const seed = await seedSignedInOrg();
    await holdSession(context, baseURL!, seed.sessionToken);
    await provide(seed);
  },
});

export { expect } from "@playwright/test";
