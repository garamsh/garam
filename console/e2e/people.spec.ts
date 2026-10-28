import type { Page } from "@playwright/test";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, holdSession, test } from "./fixtures";
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
  test("lists the organization's people alone, and invites through a dialog", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);

    // Substring on purpose: a row's accessible name is all of its
    // cells, and the assertion asks what one of them shows.
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toContainText("Owner");
    // The seeded agent is Agents' to list (ADR-0116).
    await expect(
      page.getByRole("row", { name: signedIn.agentIdentifier }),
    ).toHaveCount(0);

    const reveal = await inviteMember(page);
    await expect(reveal).toHaveAccessibleDescription(
      // Presence: the dialog's note on keeping it follows.
      /^Admits one person as Member, once\./,
    );
    await expect(reveal.locator("pre")).toHaveText(
      new RegExp(`/console/invitations/[^/]+$`),
    );
  });

  test("redirects the former Members list and a person under it to People", async ({
    page,
    signedIn,
  }) => {
    const org = `/console/orgs/${signedIn.orgSlug}`;

    await page.goto(`${org}/members`);
    await expect(page).toHaveURL(`${org}/people`);

    await page.goto(`${org}/members/${signedIn.userSlug}`);
    await expect(page).toHaveURL(`${org}/people/${signedIn.userSlug}`);
    await expect(
      page.getByRole("heading", { level: 1, name: signedIn.nickname }),
    ).toBeVisible();
  });

  test("opens a person's page, which says since when and what the caller may do", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
    await page
      .getByRole("link", { name: signedIn.nickname, exact: true })
      .click();

    await expect(page).toHaveURL(
      `/console/orgs/${signedIn.orgSlug}/people/${signedIn.userSlug}`,
    );
    await expect(
      page.getByRole("heading", { level: 1, name: signedIn.nickname }),
    ).toBeVisible();
    // The joining time is phrased, not printed as an ISO date.
    const since = page.locator("dd time").first();
    await expect(since).toHaveAttribute("datetime", /^\d{4}-\d{2}-\d{2}T/);
    await expect(
      page.getByRole("region", { name: "Your access" }),
    ).toContainText("Your organization role owner");
  });

  test("adds two people to a team at once", async ({ page, signedIn }) => {
    const member = await seedMember(signedIn);
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
    await expectHydrated(page);

    await page
      .getByRole("checkbox", { name: `Select ${signedIn.nickname}` })
      .check();
    await page
      .getByRole("checkbox", { name: `Select ${member.nickname}` })
      .check();
    await page
      .getByRole("button", { name: "Add to team", exact: true })
      .click();
    const dialog = page.getByRole("alertdialog");
    expect(await axeViolations(page, ['[role="alertdialog"]'])).toEqual([]);
    await dialog.getByRole("combobox", { name: "Team" }).click();
    await page
      .getByRole("option", { name: signedIn.groupName, exact: true })
      .click();
    await dialog
      .getByRole("button", { name: "Add to team", exact: true })
      .click();
    await expect(page.getByText("Added 2 members")).toBeVisible();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams/engineering`);
    const members = page.getByRole("region", { name: "Members" });
    await expect(members).toContainText(signedIn.nickname);
    await expect(members).toContainText(member.nickname);
  });

  test("keeps inviting on a member's page, inactive with the reason", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    const members = `/console/orgs/${signedIn.orgSlug}/people`;
    const invite = page.getByRole("button", {
      name: "Invite people",
      exact: true,
    });

    // The control: the owner's opens the dialog.
    await page.goto(members);
    await expect(invite).not.toHaveAttribute("aria-disabled");

    const member = await seedMember(signedIn);
    await context.clearCookies();
    await holdSession(context, baseURL, member.sessionToken);

    await page.goto(members);
    await expect(
      page.getByRole("row", { name: member.nickname }),
    ).toContainText("Member");
    await expect(invite).toHaveAttribute("aria-disabled", "true");
    await expect(invite).toHaveAccessibleDescription(
      "No policy lets you invite people.",
    );
  });

  for (const theme of ["light", "dark"] as const) {
    test(`the list, its invitation and a person's page pass axe in the ${theme} theme`, async ({
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

      await page.goto(
        `/console/orgs/${signedIn.orgSlug}/people/${signedIn.userSlug}`,
      );
      await expect(
        page.getByRole("region", { name: "Your access" }),
      ).toBeVisible();
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
