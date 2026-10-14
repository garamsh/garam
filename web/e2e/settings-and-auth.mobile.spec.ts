import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

/** Nothing runs off the side of a phone's screen. */
function fitsTheScreen() {
  return document.documentElement.scrollWidth <= window.innerWidth;
}

test.describe("settings and the way in, on a phone", () => {
  for (const theme of ["light", "dark"] as const) {
    test(`Settings passes axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}/settings`);
      await expect(
        page.getByRole("heading", { level: 1, name: "Settings" }),
      ).toBeVisible();

      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
      expect(await page.evaluate(fitsTheScreen)).toBe(true);
    });

    for (const path of ["/", "/login", "/register"]) {
      test(`${path} passes axe in the ${theme} theme`, async ({ page }) => {
        await useTheme(page, theme);
        await page.goto(path);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

        expect(await axeViolations(page)).toEqual([]);
        expect(await page.evaluate(fitsTheScreen)).toBe(true);
      });
    }
  }
});
