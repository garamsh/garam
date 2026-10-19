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

  test("People passes axe", async ({ page, signedIn }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
    await expect(
      // Presence: the heading carries the list's count after the title.
      page.getByRole("heading", { level: 1, name: /^People/ }),
    ).toBeVisible();

    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);
  });

  // The organizations list, creating one, and the three pages that say
  // a page is not there: inside an organization, elsewhere in the
  // console, and outside it, where no shell is drawn.
  const ROUTES = [
    { name: "/console/orgs", path: () => "/console/orgs", shell: true },
    { name: "/console/orgs/new", path: () => "/console/orgs/new", shell: true },
    {
      name: "an organization's not-found page",
      path: (org: string) => `/console/orgs/${org}/teams/no-such-team`,
      shell: true,
    },
    {
      name: "the console's not-found page",
      path: () => "/console/no-such-page",
      shell: true,
    },
    {
      name: "the not-found page outside the console",
      path: () => "/no-such-page",
      shell: false,
    },
  ];
  for (const route of ROUTES) {
    for (const theme of ["light", "dark"] as const) {
      test(`${route.name} passes axe in the ${theme} theme`, async ({
        page,
        signedIn,
      }) => {
        await useTheme(page, theme);
        await page.goto(route.path(signedIn.orgSlug));
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

        if (route.shell) await expectHydrated(page);
        expect(await axeViolations(page)).toEqual([]);
      });
    }
  }
});
