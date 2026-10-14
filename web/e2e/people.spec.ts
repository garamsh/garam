import type { Page } from "@playwright/test";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember } from "./seed";

/** Opens the invitation dialog and asks for a member's link. */
async function inviteMember(page: Page) {
  await expectHydrated(page);
  await page
    .getByRole("button", { name: "Invite people", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Invite a person" });
  await dialog.getByRole("combobox", { name: "Role", exact: true }).click();
  await page.getByRole("option", { name: "Member", exact: true }).click();
  await dialog
    .getByRole("button", { name: "Create invitation", exact: true })
    .click();
  return page.getByRole("alertdialog", { name: "Invitation link" });
}

test.describe("people", () => {
  test("lists the members by name and role, and invites through a dialog", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);

    // Substring on purpose: a row's accessible name is all of its
    // cells, and the assertion asks which role one of them shows.
    const row = page.getByRole("row", { name: signedIn.nickname });
    await expect(row).toContainText("Owner");
    // The joining time is phrased, not printed as an ISO date.
    await expect(row.locator("time")).toHaveAttribute(
      "datetime",
      /^\d{4}-\d{2}-\d{2}T/,
    );
    await expect(row).not.toContainText(/\d{4}-\d{2}-\d{2}/);

    const reveal = await inviteMember(page);
    await expect(reveal).toHaveAccessibleDescription(
      // Presence: the description goes on to say it is shown once.
      /^It admits one person as Member/,
    );
    await expect(reveal.locator("pre")).toHaveText(
      new RegExp(`/console/invitations/[^/]+$`),
    );
  });

  test("offers a member no invitation the server denies them", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    const people = `/console/orgs/${signedIn.orgSlug}/people`;

    // The control: the owner is offered it on the same page.
    await page.goto(people);
    await expect(
      page.getByRole("button", { name: "Invite people", exact: true }),
    ).toBeVisible();

    const member = await seedMember(signedIn);
    await context.clearCookies();
    await context.addCookies([
      {
        name: "garam_session",
        value: member.sessionToken,
        url: baseURL!,
        httpOnly: true,
        sameSite: "Lax",
      },
    ]);

    await page.goto(people);
    // The list reads for a member, so the page rendered and the action
    // is absent rather than unreached.
    await expect(
      page.getByRole("row", { name: member.nickname }),
    ).toContainText("Member");
    await expect(
      page.getByRole("button", { name: "Invite people", exact: true }),
    ).toBeHidden();
  });

  for (const theme of ["light", "dark"] as const) {
    test(`the People route and its invitation pass axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);

      await page
        .getByRole("button", { name: "Invite people", exact: true })
        .click();
      await expect(
        page.getByRole("dialog", { name: "Invite a person" }),
      ).toBeVisible();
      expect(await axeViolations(page, ['[role="dialog"]'])).toEqual([]);

      await page.keyboard.press("Escape");
      const reveal = await inviteMember(page);
      await expect(reveal).toBeVisible();
      expect(await axeViolations(page, ['[role="alertdialog"]'])).toEqual([]);
    });
  }
});
