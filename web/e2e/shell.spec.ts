import { expect, test } from "./fixtures";

// The console shell around pages that are not there, and the focus ring
// it gives everything a keyboard can reach.
test.describe("console shell", () => {
  test("keeps the organization around an entry that is not there", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams/no-such-team`);

    await expect(
      page.getByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeVisible();
    const breadcrumb = page.getByRole("navigation", { name: "breadcrumb" });
    await expect(
      breadcrumb.getByRole("link", { name: "Teams", exact: true }),
    ).toBeVisible();
    await expect(
      breadcrumb.getByText("Not found", { exact: true }),
    ).toBeVisible();
    await expect(breadcrumb.getByText("Team", { exact: true })).toHaveCount(0);
    await expect(
      page.getByRole("navigation", { name: "Organization sections" }),
    ).toBeVisible();
    // The organization's layout stayed mounted, so its role read did too.
    await expect(
      page.getByRole("button", { name: /Switch organization/ }),
    ).toContainText("Owner");
  });

  test("offers no sections inside an organization the caller does not hold", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}-absent`);

    await expect(
      page.getByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeVisible();
    const breadcrumb = page.getByRole("navigation", { name: "breadcrumb" });
    await expect(breadcrumb).toContainText("Organizations");
    await expect(
      breadcrumb.getByText("Not found", { exact: true }),
    ).toBeVisible();
    await expect(breadcrumb).not.toContainText("Overview");
    await expect(
      page.getByRole("navigation", { name: "Organization sections" }),
    ).toHaveCount(0);
  });

  test("draws one focus ring on a link in a page's content", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams`);
    const link = page.getByRole("link", {
      name: signedIn.groupName,
      exact: true,
    });
    await expect(link).toBeVisible();

    // A key press first, so the browser treats the focus that follows as
    // keyboard focus and applies :focus-visible.
    await page.keyboard.press("Shift");
    await link.focus();
    await expect(link).toBeFocused();

    const style = await link.evaluate((element) => {
      const computed = getComputedStyle(element);
      return {
        visible: element.matches(":focus-visible"),
        outline: computed.outlineStyle,
        shadow: computed.boxShadow,
      };
    });
    // The state the assertion is about is the one the test set up.
    expect(style.visible).toBe(true);
    expect(style.outline).toBe("none");
    expect(style.shadow).not.toBe("none");
  });
});
