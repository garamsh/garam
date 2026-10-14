import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

// The overview, Projects and Consents at 390px, in both themes.
const ROUTES = [
  { name: "the overview", segment: "" },
  { name: "Projects", segment: "/projects" },
  { name: "Consents", segment: "/consents" },
];

test.describe("organization area on a phone", () => {
  for (const route of ROUTES) {
    for (const theme of ["light", "dark"] as const) {
      test(`${route.name} passes axe in the ${theme} theme`, async ({
        page,
        signedIn,
      }) => {
        await useTheme(page, theme);
        await page.goto(`/console/orgs/${signedIn.orgSlug}${route.segment}`);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

        await expectHydrated(page);
        expect(await axeViolations(page)).toEqual([]);
        // Nothing runs off the side of a phone's screen.
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= window.innerWidth,
          ),
        ).toBe(true);
      });
    }
  }
});
