import { randomBytes } from "node:crypto";

import type { Browser, Page } from "@playwright/test";

import { expectHydrated } from "./axe";
import { expect, test } from "./fixtures";

/**
 * A password no other value in this suite carries, so a URL holding it
 * holds it because something put it there.
 */
function newPassword(): string {
  return `pw-${randomBytes(16).toString("hex")}`;
}

/**
 * Creates an invitation as the organization's owner and answers with
 * the token its link carries — what a person registering is holding.
 */
async function invitationToken(page: Page, orgSlug: string): Promise<string> {
  await page.goto(`/console/orgs/${orgSlug}/people`);
  await expectHydrated(page);
  await page.getByRole("button", { name: "Invite people" }).click();
  const dialog = page.getByRole("dialog", { name: "Invite a person" });
  await dialog.getByRole("combobox", { name: "Role" }).click();
  await page.getByRole("option", { name: "Member", exact: true }).click();
  await dialog.getByRole("button", { name: "Create invitation" }).click();

  const link = page
    .getByRole("alertdialog", { name: "Invitation link" })
    .locator("pre");
  await expect(link).toBeVisible();
  const token = ((await link.textContent()) ?? "").split("/").pop() ?? "";
  expect(token).not.toBe("");
  return token;
}

/**
 * Registers an account in a browser holding no session, at the login
 * page with nothing but the token pasted by hand — the way in for an
 * invitee who has the token and not the link. Registering lands in the
 * organization the invitation named.
 */
async function registerAccount(
  browser: Browser,
  baseURL: string | undefined,
  credential: {
    token: string;
    username: string;
    password: string;
    orgSlug: string;
  },
  urls: string[],
): Promise<void> {
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();
  page.on("request", (request) => urls.push(request.url()));

  await page.goto("/login");
  await page.getByRole("link", { name: "Create one" }).click();
  await expect(page).toHaveURL("/register");

  await page.getByLabel("Invitation token").fill(credential.token);
  await page.getByLabel("Username").fill(credential.username);
  await page.getByLabel("Password").fill(credential.password);
  await page.getByRole("button", { name: "Create account" }).click();

  await expect(page).toHaveURL(`/console/orgs/${credential.orgSlug}`);
  await context.close();
}

test.describe("password accounts", () => {
  test("registers from an invitation and signs in again with the password", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    const token = await invitationToken(page, signedIn.orgSlug);
    // The org's own slug: unique to this test, and a username the
    // character rule accepts. The account it names is a different row
    // from the seeded user of the same suffix, which the reserved
    // `local-` prefix is what keeps apart.
    const username = signedIn.orgSlug;
    const password = newPassword();
    const urls: string[] = [];

    await registerAccount(
      browser,
      baseURL,
      { token, username, password, orgSlug: signedIn.orgSlug },
      urls,
    );
    // What the filter at the end of this test rests on. `[].filter()`
    // is `[]`, so a listener attached to the wrong page or detached
    // before the first request passes it with full force; the two
    // counts here say each browser's listener collected something.
    const afterRegistering = urls.length;
    expect(afterRegistering).toBeGreaterThan(0);

    // A second browser, holding nothing the first one left behind.
    const context = await browser.newContext({ baseURL });
    const signingIn = await context.newPage();
    signingIn.on("request", (request) => urls.push(request.url()));

    await signingIn.goto("/login");
    await signingIn.getByLabel("Username").fill(username);
    await signingIn.getByLabel("Password").fill(password);
    await signingIn.getByRole("button", { name: "Sign in" }).click();

    await expect(signingIn).toHaveURL("/console/orgs");
    // The membership the invitation named, held by the account the
    // password signed in as. Substring on purpose: the card link's
    // accessible name carries the org's slug beside its name.
    await expect(
      signingIn.getByRole("link", { name: signedIn.orgName }),
    ).toBeVisible();

    // The password travelled in a body. No request either browser made
    // carries it in a URL, so none of it reached a query string, a GET,
    // or anything that logs a URL.
    expect(urls.length).toBeGreaterThan(afterRegistering);
    expect(urls.filter((url) => url.includes(password))).toEqual([]);
    expect(signingIn.url()).not.toContain(password);

    await context.close();
  });

  test("refuses a wrong password and an unknown username with one message", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    const token = await invitationToken(page, signedIn.orgSlug);
    const username = signedIn.orgSlug;

    await registerAccount(
      browser,
      baseURL,
      {
        token,
        username,
        password: newPassword(),
        orgSlug: signedIn.orgSlug,
      },
      [],
    );

    const context = await browser.newContext({ baseURL });
    const refused = await context.newPage();
    // One wrong password for both attempts, so what differs between
    // them is only whether an account holds the username.
    const wrong = newPassword();

    // Located by its text rather than by its `alert` role, which
    // Next.js's route announcer also carries.
    const refusal = refused.getByText(
      "That username and password do not match an account.",
      { exact: true },
    );

    await refused.goto("/login");
    await refused.getByLabel("Username").fill(username);
    await refused.getByLabel("Password").fill(wrong);
    await refused.getByRole("button", { name: "Sign in" }).click();
    await expect(refusal).toBeVisible();
    const wrongPassword = await refused.locator("form").innerText();

    await refused.goto("/login");
    await refused.getByLabel("Username").fill(`${username}-nobody`);
    await refused.getByLabel("Password").fill(wrong);
    await refused.getByRole("button", { name: "Sign in" }).click();
    await expect(refusal).toBeVisible();
    const unknownUsername = await refused.locator("form").innerText();

    // The two renders are the same render: garam refuses them
    // identically and the console adds nothing that tells them apart
    // (ADR-0041).
    expect(wrongPassword).toBe(unknownUsername);

    await context.close();
  });
});
