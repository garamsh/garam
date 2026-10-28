import { expect, test } from "@playwright/test";

import { axeViolations, useTheme } from "./axe";

/** Nothing runs off the side of a phone's screen. */
function fitsTheScreen() {
  return (
    document.documentElement.scrollWidth <= document.documentElement.clientWidth
  );
}

// The pages that lead into the console, at 390px in both themes.
test.describe("the way in, on a phone", () => {
  for (const theme of ["light", "dark"] as const) {
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
