import type { APIRequestContext } from "@playwright/test";
import { z } from "zod";

import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";
import type { Seed } from "./seed";

/** A policy in the test's organization, for the routes that read one. */
async function seedPolicy(request: APIRequestContext, signedIn: Seed) {
  const created = await request.post(`/api/orgs/${signedIn.orgSlug}/policies`, {
    data: {
      name: "Deny project deletion",
      document: {
        Version: "2026-07-25",
        Statement: [
          {
            Effect: "Deny",
            Action: ["project:delete"],
            Resource: [`grn:${signedIn.orgSlug}:*`],
          },
        ],
      },
    },
  });
  expect(created.status()).toBe(201);
  const { grn } = z.object({ grn: z.string() }).parse(await created.json());
  return {
    base: `/console/orgs/${signedIn.orgSlug}/permissions`,
    policy: grn.split(":")[4],
    person: signedIn.userSlug,
  };
}

// The Permissions area at 390px: every route lays out inside the
// screen, tables included, and passes axe as a phone shows it. One test
// reads one route.
const routes: {
  name: string;
  path: (area: Awaited<ReturnType<typeof seedPolicy>>) => string;
}[] = [
  { name: "the policy list", path: (a) => a.base },
  { name: "a new policy", path: (a) => `${a.base}/new` },
  { name: "the attachments view", path: (a) => `${a.base}/attachments` },
  {
    name: "a subject's attachments",
    path: (a) => `${a.base}/attachments?subject=person:${a.person}`,
  },
  { name: "a policy", path: (a) => `${a.base}/${a.policy}` },
];

test.describe("permissions on a phone", () => {
  for (const route of routes) {
    test(`${route.name} scrolls no wider than the screen and passes axe`, async ({
      page,
      signedIn,
    }) => {
      const path = route.path(await seedPolicy(page.request, signedIn));
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await expectHydrated(page);
      // The shell hydrates before this page's form, whose submit stays
      // disabled until its own part does; the scan reads it enabled.
      if (path.endsWith("/new")) {
        await expect(
          page.getByRole("button", { name: "Create draft" }),
        ).toBeEnabled();
      }

      const overflow = await page.evaluate(
        () =>
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      );
      expect(overflow).toBeLessThanOrEqual(0);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
