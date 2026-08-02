import { test as base } from "@playwright/test";

import { seedSignedInOrg, type Seed } from "./seed";

/**
 * `signedIn` gives a test a freshly seeded organization and a browser
 * context already carrying its session cookie. Each test seeds its own
 * organization, so specs never contend over shared rows.
 */
export const test = base.extend<{ signedIn: Seed }>({
  signedIn: async ({ context, baseURL }, use) => {
    const seed = await seedSignedInOrg();
    await context.addCookies([
      {
        name: "garam_session",
        value: seed.sessionToken,
        url: baseURL ?? "http://localhost:3000",
        httpOnly: true,
        sameSite: "Lax",
      },
    ]);
    await use(seed);
  },
});

export { expect } from "@playwright/test";
