import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

// The Detail template at 390px with its inactive tabs mounted and
// hidden: the page fits the screen and passes axe in either theme.
test.describe("a Detail page on a phone", () => {
  for (const theme of ["light", "dark"] as const) {
    test(`fits the screen and passes axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      await page.goto(
        `/console/orgs/${signedIn.orgSlug}/operators/${signedIn.operatorIdentifier}`,
      );
      await expectHydrated(page);
      // The theme the test asked for is the one on screen.
      await expect(page.locator("html")).toHaveClass(
        theme === "dark" ? /\bdark\b/ : /^(?!.*\bdark\b)/,
      );
      await expect(page.getByRole("tabpanel")).toHaveAccessibleName("Overview");

      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow).toBeLessThanOrEqual(0);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
