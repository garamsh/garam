import { expect, test, type BrowserContext } from "@playwright/test";

import { seedWorkspace, type Person } from "./seed";

const WEB_URL = "http://cowork.garam.localhost:3100";
const WEB_HOST = new URL(WEB_URL).hostname;
const SITE_URL = "http://garam.localhost:3001";
const SITE_HOST = new URL(SITE_URL).hostname;

/** Signs `person` in on the site's host, and nowhere else. */
async function signInOnTheSite(context: BrowserContext, person: Person) {
  await context.addCookies([
    {
      name: "garam_session",
      value: person.sessionToken,
      domain: SITE_HOST,
      path: "/",
      httpOnly: true,
    },
  ]);
}

test.describe("signing in across hosts", () => {
  test("a person signed in on the site lands signed in on cowork's host, by a session of its own", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const context = await browser.newContext();
    await signInOnTheSite(context, ws.members[0]);
    const page = await context.newPage();

    await page.goto(`${WEB_URL}/${ws.orgSlug}/tasks`);

    await expect(page).toHaveURL(`${WEB_URL}/${ws.orgSlug}/tasks`);
    await expect(
      page.getByRole("navigation", { name: "Workspace", exact: true }),
    ).toBeVisible();

    // Host-only on each of the two: a cookie with a Domain attribute
    // would read with a leading dot, and none sits on a parent domain.
    const cookies = await context.cookies();
    const sessions = cookies.filter((c) => c.name === "garam_session");
    expect(sessions.map((c) => c.domain).sort()).toEqual(
      [WEB_HOST, SITE_HOST].sort(),
    );
    const handed = sessions.find((c) => c.domain === WEB_HOST);
    expect(handed?.value).not.toBe(ws.members[0].sessionToken);
    expect(cookies.filter((c) => c.name === "garam_handoff_state")).toEqual([]);
  });

  test("a sign-out on the site ends the session on cowork's host", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const context = await browser.newContext();
    await signInOnTheSite(context, ws.members[0]);
    const page = await context.newPage();
    await page.goto(`${WEB_URL}/${ws.orgSlug}`);
    await expect(
      page.getByRole("navigation", { name: "Workspace", exact: true }),
    ).toBeVisible();

    const out = await context.request.post(`${SITE_URL}/api/auth/logout`);
    expect(out.status()).toBe(204);

    await page.goto(`${WEB_URL}/${ws.orgSlug}`);

    await expect(page).toHaveURL(
      new RegExp(`^${SITE_URL}/login\\?.*app=cowork`),
    );
    expect(new URL(page.url()).searchParams.get("next")).toBe(`/${ws.orgSlug}`);
  });

  test("a sign-in that starts on cowork's host returns there", async ({
    browser,
  }) => {
    const ws = await seedWorkspace("ada");
    const context = await browser.newContext();
    const page = await context.newPage();

    await page.goto(`${WEB_URL}/${ws.orgSlug}`);
    await expect(page).toHaveURL(
      new RegExp(`^${SITE_URL}/login\\?.*app=cowork`),
    );

    // Signing in plants the session on the site; the page then sends a
    // visitor holding one on to the app it names.
    await signInOnTheSite(context, ws.members[0]);
    await page.reload();

    await expect(page).toHaveURL(`${WEB_URL}/${ws.orgSlug}`);
    await expect(
      page.getByRole("navigation", { name: "Workspace", exact: true }),
    ).toBeVisible();
  });
});
