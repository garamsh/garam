import { expect, test } from "@playwright/test";

test.describe("signed out", () => {
  test("serves the landing page at the root", async ({ page }) => {
    await page.goto("/");

    await expect(page).toHaveURL("/");
    await expect(
      page.getByRole("heading", {
        name: "One organization for the people and the agents that work in it.",
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Open the console" }).first(),
    ).toBeVisible();
  });

  test("navigates the landing sections from the header", async ({ page }) => {
    await page.goto("/");

    const nav = page.getByRole("navigation", { name: "Sections" });
    await nav.getByRole("link", { name: "Agents" }).click();

    await expect(page).toHaveURL("/#agents");
    await expect(
      page.getByRole("heading", { name: "Stored, not yet reachable" }),
    ).toBeInViewport();
  });

  test("reaches the login page from the landing page", async ({ page }) => {
    await page.goto("/");

    await page
      .getByRole("banner")
      .getByRole("link", { name: "Sign in" })
      .click();

    await expect(page).toHaveURL("/login");
    await expect(
      page.getByRole("heading", { name: "Sign in to garam" }),
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

  test("answers both probes without a session", async ({ request }) => {
    // Published interface (ADR-0023): a scheduler holds no credential,
    // so this is the layer that proves no session is asked for.
    for (const path of ["/healthz", "/readyz"]) {
      const response = await request.get(path);

      expect(response.status(), path).toBe(200);
      expect(await response.text(), path).toBe("ok\n");
    }
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
