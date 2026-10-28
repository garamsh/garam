import { expect, test } from "./fixtures";

// Consents has no content links, so every prefetch belongs to the
// shell.
test.describe("console navigation prefetch", () => {
  test("prefetches a section only once the visitor points at it", async ({
    page,
    signedIn,
  }) => {
    const base = `/console/orgs/${signedIn.orgSlug}`;
    const prefetched: string[] = [];
    page.on("request", (request) => {
      if (request.headers()["next-router-prefetch"]) {
        prefetched.push(new URL(request.url()).pathname);
      }
    });

    await page.goto(`${base}/consents`);
    const sections = page.getByLabel("Organization sections");
    await expect(
      sections.getByRole("link", { name: "Teams", exact: true }),
    ).toBeVisible();
    await page.waitForLoadState("networkidle");
    expect(
      prefetched,
      "no section or crumb is prefetched for being on screen",
    ).toEqual([]);

    await sections.getByRole("link", { name: "Teams", exact: true }).hover();

    await expect
      .poll(() => prefetched, { message: "the pointed-at section is" })
      .toContain(`${base}/teams`);
    expect(prefetched, "a section nobody pointed at is not").not.toContain(
      `${base}/people`,
    );
  });
});
