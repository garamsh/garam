import type { Page } from "@playwright/test";

import { expectHydrated } from "./axe";
import { SITE_URL, expect, holdSession, test } from "./fixtures";
import { seedExpiredSession, seedSignedInOrg, seedSignedInUser } from "./seed";

test.describe("organization mutations", () => {
  test("creates an organization and lands on its page", async ({
    page,
    signedIn,
  }) => {
    const name = `Created Org ${signedIn.orgSlug}`;

    await page.goto("/console/orgs");
    await page.getByRole("link", { name: "New organization" }).click();

    await expect(page).toHaveURL("/console/orgs/new");
    await page.getByLabel("Name").fill(name);
    await page.getByRole("button", { name: "Create organization" }).click();

    // The backend assigns the org slug, so only the shape is asserted.
    await expect(page).toHaveURL(/\/console\/orgs\/[0-9a-f]+$/);
    await expect(
      page.getByRole("heading", { name, exact: true }),
    ).toBeVisible();

    await page.goto("/console/orgs");
    // Substring on purpose: the card link's accessible name carries the
    // org's slug beside its name, so the assertion asks only that the
    // org appears in the list.
    await expect(page.getByRole("link", { name })).toBeVisible();
  });

  test("invites a member who accepts through the link and lands in the organization", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    const invitationUrl = await createInvitationLink(page, signedIn.orgSlug);

    // A second signed-in user opens the link: the page names the
    // organization and the role before anything is accepted.
    const invitee = await seedSignedInUser();
    const inviteeContext = await browser.newContext({ baseURL });
    await holdSession(inviteeContext, baseURL, invitee.sessionToken);
    const inviteePage = await inviteeContext.newPage();
    await inviteePage.goto(invitationUrl);
    await expect(
      inviteePage.getByRole("heading", {
        level: 1,
        name: `Join ${signedIn.orgName}`,
        exact: true,
      }),
    ).toBeVisible();
    await expect(inviteePage.getByText("Joining as a member")).toBeVisible();
    await inviteePage
      .getByRole("button", { name: `Join ${signedIn.orgName}`, exact: true })
      .click();

    await expect(inviteePage).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);

    // The link works once: opened again, it says so and offers nothing
    // to press.
    await inviteePage.goto(invitationUrl);
    await expect(
      inviteePage.getByRole("heading", {
        level: 1,
        name: "This invitation has already been used",
      }),
    ).toBeVisible();
    await expect(
      inviteePage.getByRole("button", { name: /join/i }),
    ).toHaveCount(0);

    // People now lists both.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
    // Substring on purpose: a row's accessible name is all of its
    // cells, and the assertion asks which role one of them shows.
    await expect(
      page.getByRole("row", { name: invitee.userSlug }),
    ).toContainText("Member");

    await inviteeContext.close();
  });

  test("keeps the invitation through register when the link is opened signed out", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    const invitationUrl = await createInvitationLink(page, signedIn.orgSlug);
    const token = new URL(invitationUrl).pathname.split("/").pop() ?? "";
    expect(token).not.toBe("");

    const context = await browser.newContext({ baseURL });
    const visitor = await context.newPage();
    await visitor.goto(invitationUrl);

    // The proxy sends a visitor without a session to sign in, carrying
    // the invitation along, and the page says what it is for.
    await expect(visitor).toHaveURL(
      `${SITE_URL}/login?next=${encodeURIComponent(`/console/invitations/${token}`)}`,
    );
    await expect(
      visitor.getByRole("heading", {
        name: `Sign in to join ${signedIn.orgName}`,
        exact: true,
      }),
    ).toBeVisible();

    await visitor.getByRole("link", { name: "Create one" }).click();
    await expect(
      visitor.getByRole("heading", {
        name: `Create an account to join ${signedIn.orgName}`,
        exact: true,
      }),
    ).toBeVisible();
    // Nobody copies the token by hand: the form does not ask for it.
    await expect(visitor.getByLabel("Invitation token")).toHaveCount(0);

    // The org's slug is unique to this test and a username the
    // character rule accepts.
    await visitor.getByLabel("Username").fill(`r-${signedIn.orgSlug}`);
    await visitor
      .getByLabel("Password")
      .fill(`pw-${signedIn.orgSlug}-register`);
    await visitor.getByRole("button", { name: "Create account" }).click();

    await expect(visitor).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);
    await expect(
      visitor.getByRole("heading", {
        level: 1,
        name: signedIn.orgName,
        exact: true,
      }),
    ).toBeVisible();

    await context.close();
  });

  test("keeps the invitation through sign-in when the link is opened signed out", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    // An account that already exists, in another organization: it
    // registers through an invitation of the seeded one first.
    const username = `s-${signedIn.orgSlug}`;
    const password = `pw-${signedIn.orgSlug}-sign-in`;
    const first = await createInvitationLink(page, signedIn.orgSlug);
    const registering = await browser.newContext({ baseURL });
    const registerPage = await registering.newPage();
    await registerPage.goto(first);
    await registerPage.getByRole("link", { name: "Create one" }).click();
    // The login page holds a Username field too, so the register page's
    // heading is what says the fields below are the register form's.
    await expect(
      registerPage.getByRole("heading", {
        name: `Create an account to join ${signedIn.orgName}`,
        exact: true,
      }),
    ).toBeVisible();
    await registerPage.getByLabel("Username").fill(username);
    await registerPage.getByLabel("Password").fill(password);
    await registerPage.getByRole("button", { name: "Create account" }).click();
    await expect(registerPage).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);
    await registering.close();

    // A second organization's owner invites that account.
    const other = await seedSignedInOrg();
    const ownerContext = await browser.newContext({ baseURL });
    await holdSession(ownerContext, baseURL, other.sessionToken);
    const second = await createInvitationLink(
      await ownerContext.newPage(),
      other.orgSlug,
    );
    await ownerContext.close();

    const context = await browser.newContext({ baseURL });
    const visitor = await context.newPage();
    await visitor.goto(second);
    await expect(
      visitor.getByRole("heading", {
        name: `Sign in to join ${other.orgName}`,
        exact: true,
      }),
    ).toBeVisible();
    await visitor.getByLabel("Username").fill(username);
    await visitor.getByLabel("Password").fill(password);
    await visitor.getByRole("button", { name: "Sign in", exact: true }).click();

    // Signing in returns to the invitation rather than the front page.
    await expect(visitor).toHaveURL(new URL(second).pathname);
    await visitor
      .getByRole("button", { name: `Join ${other.orgName}`, exact: true })
      .click();
    await expect(visitor).toHaveURL(`/console/orgs/${other.orgSlug}`);

    await context.close();
  });

  test("keeps the invitation through sign-in when the browser's session has expired", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    // An account that already exists, through an invitation of the
    // seeded organization.
    const username = `x-${signedIn.orgSlug}`;
    const password = `pw-${signedIn.orgSlug}-expired`;
    const first = await createInvitationLink(page, signedIn.orgSlug);
    const registering = await browser.newContext({ baseURL });
    const registerPage = await registering.newPage();
    await registerPage.goto(first);
    await registerPage.getByRole("link", { name: "Create one" }).click();
    await expect(
      registerPage.getByRole("heading", {
        name: `Create an account to join ${signedIn.orgName}`,
        exact: true,
      }),
    ).toBeVisible();
    await registerPage.getByLabel("Username").fill(username);
    await registerPage.getByLabel("Password").fill(password);
    await registerPage.getByRole("button", { name: "Create account" }).click();
    await expect(registerPage).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);
    await registering.close();

    const other = await seedSignedInOrg();
    const ownerContext = await browser.newContext({ baseURL });
    await holdSession(ownerContext, baseURL, other.sessionToken);
    const second = await createInvitationLink(
      await ownerContext.newPage(),
      other.orgSlug,
    );
    await ownerContext.close();

    // The browser still holds a cookie, so the proxy lets the request
    // through; the backend no longer honours the session behind it.
    const stale = await seedExpiredSession(
      `grn:root:default:user:local-${username}`,
    );
    const context = await browser.newContext({ baseURL });
    await holdSession(context, baseURL, stale);
    const visitor = await context.newPage();
    const invitationPath = new URL(second).pathname;
    await visitor.goto(second);

    await expect(visitor).toHaveURL(
      `${SITE_URL}/login?next=${encodeURIComponent(invitationPath)}`,
    );
    await expect(
      visitor.getByRole("heading", {
        name: `Sign in to join ${other.orgName}`,
        exact: true,
      }),
    ).toBeVisible();
    await visitor.getByLabel("Username").fill(username);
    await visitor.getByLabel("Password").fill(password);
    await visitor.getByRole("button", { name: "Sign in", exact: true }).click();

    await expect(visitor).toHaveURL(invitationPath);
    await visitor
      .getByRole("button", { name: `Join ${other.orgName}`, exact: true })
      .click();
    await expect(visitor).toHaveURL(`/console/orgs/${other.orgSlug}`);

    await context.close();
  });
});

/**
 * Creates a member invitation as the organization's owner and answers
 * with the link People shows to send.
 */
async function createInvitationLink(page: Page, orgSlug: string) {
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
  return (await link.textContent()) ?? "";
}
