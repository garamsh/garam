import { test as base, type BrowserContext } from "@playwright/test";

import { seedSignedInOrg, type Seed } from "./seed";

/** Where the site is served, beside the console this suite drives. */
export const SITE_URL =
  process.env.GARAM_E2E_SITE_URL ?? "http://garam.localhost:3001";

/**
 * Plants `session` on the console's host alone, as the handoff's
 * redemption plants it: the cookie is host-only (ADR-0131), so the
 * site never reads it.
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
      url: consoleURL ?? "http://console.garam.localhost:3000",
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
