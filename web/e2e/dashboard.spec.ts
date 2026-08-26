import { expect, test } from "./fixtures";

test.describe("signed in", () => {
  test("lists the caller's organizations", async ({ page, signedIn }) => {
    await page.goto("/console/orgs");

    await expect(
      page.getByRole("heading", { name: "Organizations", exact: true }),
    ).toBeVisible();
    // Substring on purpose: the card link's accessible name carries the
    // org's slug beside its name, so the assertion asks only that the
    // org appears in the list.
    await expect(
      page.getByRole("link", { name: signedIn.orgName }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.nickname, { exact: true }),
    ).toBeVisible();
  });

  test("opens an organization and counts what it holds", async ({
    page,
    signedIn,
  }) => {
    await page.goto("/console/orgs");
    await page.getByRole("link", { name: signedIn.orgName }).click();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);
    await expect(
      page.getByRole("heading", { name: signedIn.orgName, exact: true }),
    ).toBeVisible();
    // Substring on purpose: the breadcrumb is a trail, and the org is
    // one item in it.
    await expect(
      page.getByRole("navigation", { name: "breadcrumb" }),
    ).toContainText(signedIn.orgName);
    // Substring on purpose: the card's accessible name is the label plus
    // the count, and a trailing count must not break the navigation
    // assertion — the count itself is what the assertion fixes.
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
    // Substring on purpose: a row's accessible name is all of its
    // cells, and the assertion asks which role one of them shows.
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

  test("keeps the collapsed sidebar collapsed across a reload", async ({
    page,
    signedIn,
  }) => {
    const collapsed = page.locator(
      '[data-slot="sidebar"][data-state="collapsed"]',
    );
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);
    // A click the toggle takes before React has hydrated it is dropped,
    // not queued, so repeat it until the sidebar reports collapsed: the
    // state the reload has to carry is set by a click that landed.
    await expect(async () => {
      await page
        .getByRole("button", { name: "Toggle Sidebar" })
        .first()
        .click();
      await expect(collapsed).toHaveCount(1, { timeout: 1_000 });
    }).toPass({ timeout: 15_000 });

    await page.reload();

    await expect(collapsed).toHaveCount(1);
    // The markup the reload was served, before anything on the client
    // could correct it: a sidebar collapsed only after hydration would
    // still be expanded here, and would jump on every load.
    const served = await page.request.get(`/console/orgs/${signedIn.orgSlug}`);
    const html = await served.text();
    expect(html).toContain('data-state="collapsed"');
    expect(html).not.toContain('data-state="expanded"');
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
    // The heading rather than a provider button: which buttons the page
    // offers is the backend's answer (ADR-0010), and this stack
    // registers no provider.
    await expect(
      page.getByRole("heading", { name: "Sign in to garam", exact: true }),
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
