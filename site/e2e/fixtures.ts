import { test as base, type BrowserContext } from "@playwright/test";

import { seedSignedInOrg, type Seed } from "./seed";

/** Where the console is served, beside the site this suite drives. */
export const CONSOLE_URL =
  process.env.GARAM_E2E_CONSOLE_URL ?? "http://console.garam.localhost:3000";

/**
 * Plants `session` on the site's host alone, as a sign-in here plants
 * it: the cookie is host-only (ADR-0131), and the console is handed a
 * session of its own.
 */
export async function holdSession(
  context: BrowserContext,
  siteURL: string,
  session: string,
): Promise<void> {
  await context.addCookies([
    {
      name: "garam_session",
      value: session,
      url: siteURL,
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
