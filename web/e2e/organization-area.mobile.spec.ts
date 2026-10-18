import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";

// Home, Projects and a project's page, Consents, Teams, People and
// agents and a person's page, and Hosts at 390px, in both themes.
// `engineering` and `frontend` are the seeded team and project, and
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
  { name: "Hosts", segment: "/hosts" },
  { name: "a project's page", segment: "/projects/frontend" },
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
