import { axeViolations } from "./axe";
import { expect, test } from "./fixtures";

// The console at 390px: the sidebar is a sheet behind the trigger, and
// the top bar alone has to say which organization a page acts on.
test.describe("console shell on a phone", () => {
  test("keeps the organization's name in the top bar", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams`);

    const breadcrumb = page.getByRole("navigation", { name: "breadcrumb" });
    await expect(
      breadcrumb.getByRole("link", { name: signedIn.orgName, exact: true }),
    ).toBeVisible();
    await expect(breadcrumb.getByText("Teams", { exact: true })).toBeVisible();
    // Control: the crumb above the organization gives way for it.
    await expect(
      breadcrumb.getByRole("link", { name: "Organizations", exact: true }),
    ).toBeHidden();
  });

  test("opens the sidebar as a sheet and moves between sections", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);
    const nav = page.getByRole("navigation", { name: "Organization sections" });
    await expect(nav).toBeHidden();

    await expect(async () => {
      await page.getByRole("button", { name: "Toggle Sidebar" }).first().tap();
      await expect(nav).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });
    await nav.getByRole("link", { name: "People", exact: true }).tap();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/people`);
  });

  test("scrolls no wider than the screen", async ({ page, signedIn }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("the shell passes axe", async ({ page, signedIn }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

    expect(await axeViolations(page, ["header"])).toEqual([]);
  });
});
