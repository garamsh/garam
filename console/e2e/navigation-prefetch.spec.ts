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

// A return path can hold an invitation token, and a prefetch is a
// request carrying it that nobody asked for.
test.describe("sign-in shell prefetch", () => {
  for (const { from, link } of [
    { from: "/login", link: "Create one" },
    { from: "/register", link: "Sign in" },
  ]) {
    test(`does not prefetch ${from}'s "${link}" link, which carries the token`, async ({
      page,
    }) => {
      const token = `prefetch-${Date.now()}`;
      const next = `/console/invitations/${token}`;
      const carrying: string[] = [];
      const prefetched: string[] = [];
      page.on("request", (request) => {
        if (request.url().includes(token)) carrying.push(request.url());
        if (request.headers()["next-router-prefetch"]) {
          prefetched.push(request.url());
        }
      });

      await page.goto(`${from}?next=${encodeURIComponent(next)}`);
      const target = page.getByRole("link", { name: link, exact: true });
      await expect(target).toHaveAttribute(
        "href",
        new RegExp(encodeURIComponent(token)),
      );
      // Pointing at the link is what an intent prefetch waits for.
      await target.hover();
      await target.focus();
      await page.waitForLoadState("networkidle");

      // The control: the page itself prefetches something, so the list
      // below is empty because the link is excluded, not because no
      // prefetch ever runs here.
      expect(
        prefetched.length,
        "this page prefetches its other links",
      ).toBeGreaterThan(0);
      expect(
        carrying.filter(
          (url) => !url.endsWith(`?next=${encodeURIComponent(next)}`),
        ),
        "no request but the page's own carries the token",
      ).toEqual([]);
    });
  }
});
