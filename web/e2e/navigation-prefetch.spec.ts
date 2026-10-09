import { expect, test } from "./fixtures";

// The sidebar's sections prefetch only once the visitor shows intent
// (#1209): a console page that merely shows them renders none of them on
// the server, and the section the pointer rests on is prefetched before
// the click. Prefetching runs only in a production build, which is what
// this suite serves.
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

    await page.goto(base);
    const sections = page.getByLabel("Organization sections");
    await expect(sections.getByRole("link", { name: "Teams" })).toBeVisible();
    await page.waitForLoadState("networkidle");
    expect(
      prefetched.filter((path) => path.startsWith(`${base}/`)),
      "no section is prefetched for being on screen",
    ).toEqual([]);

    await sections.getByRole("link", { name: "Teams" }).hover();

    await expect
      .poll(() => prefetched, { message: "the pointed-at section is" })
      .toContain(`${base}/teams`);
    expect(prefetched, "a section nobody pointed at is not").not.toContain(
      `${base}/people`,
    );
  });
});
