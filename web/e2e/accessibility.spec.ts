import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember } from "./seed";

// The console shell in both themes, one existing page in full, and
// the identifiers its table offers to copy.
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

  test("the People table's identifiers are 24px targets", async ({
    page,
    signedIn,
  }) => {
    // A second member, so each identifier has a neighbour to crowd.
    await seedMember(signedIn);
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
    await expectHydrated(page);
    const copies = page
      .getByRole("table")
      .getByRole("button", { name: /^Copy the identifier of/ });
    await expect(copies).toHaveCount(2);

    // axe's target-size passes a smaller target spaced off its
    // neighbours, so the size itself is measured as well.
    for (const copy of await copies.all()) {
      const box = await copy.boundingBox();
      expect(box?.width).toBeGreaterThanOrEqual(24);
      expect(box?.height).toBeGreaterThanOrEqual(24);
    }
    // WCAG 2.2 AA, which carries target-size.
    expect(await axeViolations(page, ["table"])).toEqual([]);
  });
});
