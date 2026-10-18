import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

// Home, Projects, Consents, Teams, People and agents and a person's
// page at 390px, in both themes. `engineering` is the seeded team, and
// `SELF` stands for the signed-in person's identifier.
const ROUTES = [
  { name: "Home", segment: "" },
  { name: "Projects", segment: "/projects" },
  { name: "Consents", segment: "/consents" },
  { name: "Teams", segment: "/teams" },
  { name: "a new team", segment: "/teams/new" },
  { name: "a team's page", segment: "/teams/engineering" },
  { name: "People and agents", segment: "/members" },
  { name: "a person's page", segment: "/members/SELF" },
];

test.describe("organization area on a phone", () => {
  for (const route of ROUTES) {
    for (const theme of ["light", "dark"] as const) {
      test(`${route.name} passes axe in the ${theme} theme`, async ({
        page,
        signedIn,
      }) => {
        await useTheme(page, theme);
        await page.goto(
          `/console/orgs/${signedIn.orgSlug}${route.segment.replace("SELF", signedIn.userSlug)}`,
        );
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

        await expectHydrated(page);
        expect(await axeViolations(page)).toEqual([]);
        // Nothing runs off the side of a phone's screen.
        expect(
          await page.evaluate(
            () =>
              document.documentElement.scrollWidth <=
              document.documentElement.clientWidth,
          ),
        ).toBe(true);
      });
    }
  }
});
