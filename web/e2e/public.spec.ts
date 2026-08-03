import { expect, test } from "@playwright/test";

test.describe("signed out", () => {
  test("serves the landing page at the root", async ({ page }) => {
    await page.goto("/");

    await expect(page).toHaveURL("/");
    await expect(
      page.getByRole("heading", {
        name: "Who belongs to your organization, and what they may do.",
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Open the console" }).first(),
    ).toBeVisible();
  });

  test("redirects a console page to the login page", async ({ page }) => {
    await page.goto("/console/orgs");

    await expect(page).toHaveURL("/login");
    await expect(
      page.getByRole("heading", { name: "Sign in to garam" }),
    ).toBeVisible();
  });

  test("starts the backend OAuth flow from the Google sign-in button", async ({
    page,
  }) => {
    await page.goto("/login");

    // Google's branding guidelines: its own button asset under an
    // approved label
    // (developers.google.com/identity/branding-guidelines).
    const button = page.getByRole("link", { name: "Sign in with Google" });
    await expect(button).toHaveAttribute("href", "/api/auth/login/google");
    await expect(button.locator("img").first()).toBeVisible();
  });

  test("serves the backend through the same-origin /api proxy", async ({
    request,
  }) => {
    // The proxy is what keeps the session cookie same-origin; a 401
    // from `/me` proves the rewrite reaches the real backend.
    const response = await request.get("/api/me");

    expect(response.status()).toBe(401);
    expect(await response.json()).toMatchObject({ kind: "unauthenticated" });
  });
});
