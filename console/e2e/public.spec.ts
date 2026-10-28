import { expect, test } from "@playwright/test";

test.describe("signed out", () => {
  test("serves the landing page at the root", async ({ page }) => {
    await page.goto("/");

    await expect(page).toHaveURL("/");
    await expect(
      page.getByRole("heading", {
        name: "One organization for the people and the agents that work in it.",
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Open the console", exact: true }).first(),
    ).toBeVisible();
  });

  test("navigates the landing sections from the header", async ({ page }) => {
    await page.goto("/");

    const nav = page.getByRole("navigation", { name: "Sections" });
    await nav.getByRole("link", { name: "Agents" }).click();

    await expect(page).toHaveURL("/#agents");
    await expect(
      page.getByRole("heading", {
        name: "Reachable, not yet watched",
        exact: true,
      }),
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
      page.getByRole("heading", { name: "Sign in to garam", exact: true }),
    ).toBeVisible();
  });

  test("redirects a console page to the login page, carrying where it was going", async ({
    page,
  }) => {
    await page.goto("/console/orgs");

    await expect(page).toHaveURL("/login?next=%2Fconsole%2Forgs");
    await expect(
      page.getByRole("heading", { name: "Sign in to garam", exact: true }),
    ).toBeVisible();
  });

  test("offers no provider button when the backend registers none", async ({
    page,
  }) => {
    await page.goto("/login");

    // This stack sets no GOOGLE_CLIENT_ID
    // (`deploy/local/.env.example`), so `serve api` registers no
    // provider and `/auth/login/google` answers 404. The page asks
    // `/auth/providers` and offers what the backend answers with
    // (ADR-0010), so the button is absent here and the password form is
    // the way in.
    await expect(
      page.getByRole("heading", { name: "Sign in to garam", exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel("Username")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Sign in with Google" }),
    ).toHaveCount(0);
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
