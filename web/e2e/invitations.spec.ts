import { expect, test } from "./fixtures";
import { seedSignedInUser } from "./seed";

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

  test("invites a member who accepts through the link", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    // The owner creates an invitation and copies its link.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);
    await page.getByLabel("Role").click();
    await page.getByRole("option", { name: "Member" }).click();
    await page.getByRole("button", { name: "Create invitation" }).click();

    const linkField = page.getByLabel("Invitation link");
    await expect(linkField).toBeVisible();
    const invitationUrl = await linkField.inputValue();

    // A second signed-in user opens the link and accepts.
    const invitee = await seedSignedInUser();
    const inviteeContext = await browser.newContext({ baseURL });
    await inviteeContext.addCookies([
      {
        name: "garam_session",
        value: invitee.sessionToken,
        url: baseURL ?? "http://localhost:3000",
        httpOnly: true,
        sameSite: "Lax",
      },
    ]);
    const inviteePage = await inviteeContext.newPage();
    await inviteePage.goto(invitationUrl);
    await inviteePage
      .getByRole("button", { name: "Accept invitation" })
      .click();

    await expect(inviteePage).toHaveURL("/console/orgs");
    await expect(
      inviteePage.getByRole("link", { name: signedIn.orgName }),
    ).toBeVisible();

    // The members section now lists both members.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);
    // Substring on purpose: a row's accessible name is all of its
    // cells, and the assertion asks which role one of them shows.
    await expect(
      page.getByRole("row", { name: invitee.userSlug }),
    ).toContainText("member");

    await inviteeContext.close();
  });
});
