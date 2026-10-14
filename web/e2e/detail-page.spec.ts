import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

// The Detail template's tabs, on the operator page: an inactive tab is
// hidden from sight and from the accessibility tree, yet stays mounted,
// so what a person typed in it is there on return.
test.describe("a Detail page's tabs", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  for (const theme of ["light", "dark"] as const) {
    test(`keep an unsaved edit across a switch and pass axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      const operator = `/console/orgs/${signedIn.orgSlug}/operators/${signedIn.operatorIdentifier}`;
      await page.goto(`${operator}?tab=settings`);
      await expectHydrated(page);
      // The theme the test asked for is the one on screen.
      await expect(page.locator("html")).toHaveClass(
        theme === "dark" ? /\bdark\b/ : /^(?!.*\bdark\b)/,
      );

      // The deep link opened Settings, and only it is in view.
      const panels = page.getByRole("tabpanel");
      await expect(panels).toHaveCount(1);
      await expect(panels).toHaveAccessibleName("Settings");
      const name = page.getByRole("textbox", { name: "Name" });
      await name.fill("Unsaved name");

      await page.getByRole("tab", { name: "Overview", exact: true }).click();
      await expect(panels).toHaveCount(1);
      await expect(panels).toHaveAccessibleName("Overview");
      await expect(name).toBeHidden();
      expect(await axeViolations(page)).toEqual([]);

      await page.getByRole("tab", { name: "Settings", exact: true }).click();
      await expect(name).toHaveValue("Unsaved name");
    });
  }
});
