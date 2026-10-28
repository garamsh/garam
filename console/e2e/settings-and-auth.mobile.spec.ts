import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

/** Nothing runs off the side of a phone's screen. */
function fitsTheScreen() {
  return (
    document.documentElement.scrollWidth <= document.documentElement.clientWidth
  );
}

// Settings, the organizations list, an invitation, and the pages that
// lead into the console, at 390px in both themes.
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

    test(`/console/orgs passes axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      await page.goto("/console/orgs");
      await expect(
        page.getByRole("link", { name: signedIn.orgName }),
      ).toBeVisible();

      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
      expect(await page.evaluate(fitsTheScreen)).toBe(true);
    });

    test(`an invitation passes axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      const response = await page.request.post(
        `/api/orgs/${signedIn.orgSlug}/invitations`,
        { data: { role: "member" } },
      );
      expect(response.status()).toBe(201);
      const { token } = (await response.json()) as { token: string };
      await useTheme(page, theme);
      await page.goto(`/console/invitations/${token}`);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

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
