import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

// The console shell in both themes, and one existing page in full.
test.describe("accessibility", () => {
  for (const theme of ["light", "dark"] as const) {
    test(`the console shell passes axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}`);
      await expect(
        page.getByRole("navigation", { name: "Organization sections" }),
      ).toBeVisible();
      // The theme the test asked for is the one on screen.
      await expect(page.locator("html")).toHaveClass(
        theme === "dark" ? /\bdark\b/ : /^(?!.*\bdark\b)/,
      );

      await expectHydrated(page);
      expect(
        await axeViolations(page, ['[data-slot="sidebar"]', "header"]),
      ).toEqual([]);
    });
  }

  test("the command palette passes axe", async ({ page, signedIn }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);
    await page.getByRole("button", { name: /Search or jump to/ }).click();
    await expect(
      page.getByRole("dialog", { name: "Search or jump to" }),
    ).toBeVisible();

    expect(await axeViolations(page, ['[role="dialog"]'])).toEqual([]);
  });

  test("the People page passes axe", async ({ page, signedIn }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
    await expect(
      page.getByRole("heading", { level: 1, name: "People" }),
    ).toBeVisible();

    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);
  });
});
