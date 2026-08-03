import { expect, test } from "@playwright/test";

test.describe("signed out", () => {
  test("redirects a protected page to the login page", async ({ page }) => {
    await page.goto("/orgs");

    await expect(page).toHaveURL("/login");
    await expect(
      page.getByRole("heading", { name: "Sign in to garam" }),
    ).toBeVisible();
  });

  test("redirects the root to the login page", async ({ page }) => {
    await page.goto("/");

    await expect(page).toHaveURL("/login");
  });

  test("starts the backend OAuth flow from the sign-in button", async ({
    page,
  }) => {
    await page.goto("/login");

    await expect(
      page.getByRole("link", { name: "Sign in with Google" }),
    ).toHaveAttribute("href", "/api/auth/login/google");
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
