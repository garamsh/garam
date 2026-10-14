import { test, expect } from "@playwright/test";

import { axeViolations, useTheme } from "./axe";

// The signed-out routes in both themes, and the register form again
// with a refusal on one of its fields.
const ROUTES = [
  { path: "/", heading: "One organization for the people" },
  { path: "/login", heading: "Sign in to garam" },
  { path: "/register", heading: "Create your garam account" },
];

test.describe("signed-out pages", () => {
  for (const theme of ["light", "dark"] as const) {
    for (const route of ROUTES) {
      test(`${route.path} passes axe in the ${theme} theme`, async ({
        page,
      }) => {
        await useTheme(page, theme);
        await page.goto(route.path);
        await expect(
          page.getByRole("heading", { level: 1, name: route.heading }),
        ).toBeVisible();

        expect(await axeViolations(page)).toEqual([]);
      });
    }

    test(`the register form passes axe with a refusal on its field in the ${theme} theme`, async ({
      page,
    }) => {
      await useTheme(page, theme);
      await page.goto("/register");
      await page.getByLabel("Invitation token").fill("no-such-invitation");
      await page.getByLabel("Username").fill("visitor");
      await page.getByLabel("Password").fill("a-password-long-enough");
      await page.getByRole("button", { name: "Create account" }).click();

      const token = page.getByLabel("Invitation token");
      await expect(token).toHaveAttribute("aria-invalid", "true");
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
