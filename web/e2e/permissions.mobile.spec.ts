import { z } from "zod";

import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";

// The Permissions area at 390px: every route lays out inside the
// screen, tables included, and passes axe as a phone shows it.
test.describe("permissions on a phone", () => {
  test("every route scrolls no wider than the screen and passes axe", async ({
    page,
    signedIn,
  }) => {
    const created = await page.request.post(
      `/api/orgs/${signedIn.orgSlug}/policies`,
      {
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
      },
    );
    expect(created.status()).toBe(201);
    const { grn } = z.object({ grn: z.string() }).parse(await created.json());
    const base = `/console/orgs/${signedIn.orgSlug}/permissions`;

    for (const path of [
      base,
      `${base}/new`,
      `${base}/attachments`,
      `${base}/attachments?subject=person:${signedIn.userSlug}`,
      `${base}/${grn.split(":")[4]}`,
    ]) {
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
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow, path).toBeLessThanOrEqual(0);
      expect(await axeViolations(page), path).toEqual([]);
    }
  });
});
