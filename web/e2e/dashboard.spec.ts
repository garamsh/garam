import { expect, test } from "./fixtures";

test.describe("signed in", () => {
  test("lists the caller's organizations", async ({ page, signedIn }) => {
    await page.goto("/console/orgs");

    await expect(
      page.getByRole("heading", { name: "Organizations" }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: signedIn.orgName }),
    ).toBeVisible();
    await expect(page.getByText(signedIn.nickname)).toBeVisible();
  });

  test("opens an organization and counts what it holds", async ({
    page,
    signedIn,
  }) => {
    await page.goto("/console/orgs");
    await page.getByRole("link", { name: signedIn.orgName }).click();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);
    await expect(
      page.getByRole("heading", { name: signedIn.orgName }),
    ).toBeVisible();
    await expect(
      page.getByRole("navigation", { name: "breadcrumb" }),
    ).toContainText(signedIn.orgName);
    await expect(
      page.getByRole("main").getByRole("link", { name: "Groups" }),
    ).toContainText("1");
  });

  test("moves between the organization's sections from the sidebar", async ({
    page,
    signedIn,
  }) => {
    const nav = page.getByRole("list", { name: "Organization sections" });
    const breadcrumb = page.getByRole("navigation", { name: "breadcrumb" });
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);

    await nav.getByRole("link", { name: "Groups" }).click();
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/groups`);
    await expect(
      page.getByText(signedIn.groupName, { exact: true }),
    ).toBeVisible();

    await nav.getByRole("link", { name: "Projects" }).click();
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/projects`);
    await expect(
      page.getByText(signedIn.projectName, { exact: true }),
    ).toBeVisible();

    await nav.getByRole("link", { name: "Members" }).click();
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/members`);
    await expect(
      page.getByRole("row", { name: signedIn.userSlug }),
    ).toContainText("owner");

    await expect(breadcrumb).toContainText("Members");
    await expect(nav.getByRole("link", { name: "Members" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  test("switches organizations from the sidebar header", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/groups`);

    await page.getByRole("button", { name: "Switch organization" }).click();
    await expect(
      page.getByRole("menuitem", { name: signedIn.orgName }),
    ).toHaveAttribute("aria-current", "true");
    await page.getByRole("menuitem", { name: "New organization" }).click();

    await expect(page).toHaveURL("/console/orgs/new");
  });

  test("collapses the sidebar to icons", async ({ page, signedIn }) => {
    const nav = page.getByRole("list", { name: "Organization sections" });
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);
    await expect(nav.getByRole("link", { name: "Groups" })).toBeVisible();

    await page.getByRole("button", { name: "Toggle Sidebar" }).first().click();

    await expect(
      page.locator('[data-slot="sidebar"][data-state="collapsed"]'),
    ).toHaveCount(1);
    // Collapsing to icons hides the labels, not the links.
    await expect(nav.getByRole("link", { name: "Groups" })).toBeVisible();
  });

  test("lands on the dashboard from the post-login redirect target", async ({
    page,
    signedIn,
  }) => {
    expect(signedIn.orgSlug).toBeTruthy();

    // The backend redirects to `/console` after the OAuth callback.
    await page.goto("/console");

    await expect(page).toHaveURL("/console/orgs");
  });

  test("skips the login page once signed in", async ({ page, signedIn }) => {
    expect(signedIn.sessionToken).toBeTruthy();

    await page.goto("/login");

    await expect(page).toHaveURL("/console/orgs");
  });

  test("signs out back to the login page", async ({ page, signedIn }) => {
    await page.goto("/console/orgs");
    await page.getByRole("button", { name: "Open account menu" }).click();
    await page.getByRole("menuitem", { name: "Sign out" }).click();

    await expect(page).toHaveURL("/login");
    await expect(
      page.getByRole("link", { name: "Sign in with Google" }),
    ).toBeVisible();

    // The session is revoked server-side, not merely dropped by the
    // browser: replacing the cookie does not get back in.
    await page.context().addCookies([
      {
        name: "garam_session",
        value: signedIn.sessionToken,
        url: "http://localhost:3000",
      },
    ]);
    await page.goto("/console/orgs");
    await expect(page).toHaveURL("/login");
  });
});
