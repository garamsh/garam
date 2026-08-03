import { expect, test } from "./fixtures";

test.describe("signed in", () => {
  test("lists the caller's organizations", async ({ page, signedIn }) => {
    await page.goto("/orgs");

    await expect(
      page.getByRole("heading", { name: "Organizations" }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: signedIn.orgName }),
    ).toBeVisible();
    await expect(page.getByText(signedIn.nickname)).toBeVisible();
  });

  test("opens an organization and shows its groups, projects and members", async ({
    page,
    signedIn,
  }) => {
    await page.goto("/orgs");
    await page.getByRole("link", { name: signedIn.orgName }).click();

    await expect(page).toHaveURL(`/orgs/${signedIn.orgSlug}`);
    await expect(
      page.getByRole("heading", { name: signedIn.orgName }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Groups" }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.groupName, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.projectName, { exact: true }),
    ).toBeVisible();

    const memberRow = page.getByRole("row", { name: signedIn.userGrn });
    await expect(memberRow).toContainText("owner");
  });

  test("lands on the dashboard from the post-login redirect target", async ({
    page,
    signedIn,
  }) => {
    expect(signedIn.orgSlug).toBeTruthy();

    // The backend redirects to `/` after the OAuth callback.
    await page.goto("/");

    await expect(page).toHaveURL("/orgs");
  });

  test("signs out back to the login page", async ({ page, signedIn }) => {
    await page.goto("/orgs");
    await page.getByRole("button", { name: "Sign out" }).click();

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
    await page.goto("/orgs");
    await expect(page).toHaveURL("/login");
  });
});
