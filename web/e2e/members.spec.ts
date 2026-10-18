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

test.describe("people and agents", () => {
  test("lists people and agents together, and invites through a dialog", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);

    // Substring on purpose: a row's accessible name is all of its
    // cells, and the assertion asks what one of them shows.
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toContainText("Owner");
    // The seeded agent reports failed, and reads as one state.
    await expect(
      page.getByRole("row", { name: signedIn.agentIdentifier }),
    ).toContainText("Failing");

    const reveal = await inviteMember(page);
    await expect(reveal).toHaveAccessibleDescription(
      // Presence: the description goes on to say it is shown once.
      /^It admits one person as Member/,
    );
    await expect(reveal.locator("pre")).toHaveText(
      new RegExp(`/console/invitations/[^/]+$`),
    );
  });

  test("redirects the former People and Agents lists here, by kind", async ({
    page,
    signedIn,
  }) => {
    const org = `/console/orgs/${signedIn.orgSlug}`;

    await page.goto(`${org}/people`);
    await expect(page).toHaveURL(`${org}/members?kind=person`);

    await page.goto(`${org}/agents`);
    await expect(page).toHaveURL(`${org}/members?kind=agent`);
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toHaveCount(0);
  });

  test("opens a person's page, which says since when and what the caller may do", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);
    await page
      .getByRole("link", { name: signedIn.nickname, exact: true })
      .click();

    await expect(page).toHaveURL(
      `/console/orgs/${signedIn.orgSlug}/members/${signedIn.userSlug}`,
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

  test("adds a person and an agent to a team at once", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);
    await expectHydrated(page);

    await page
      .getByRole("checkbox", { name: `Select ${signedIn.nickname}` })
      .check();
    await page.getByRole("checkbox", { name: "Select Unnamed agent" }).check();
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
    await expect(members).toContainText("Unnamed agent");
  });

  test("keeps inviting on a member's page, inactive with the reason", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    const members = `/console/orgs/${signedIn.orgSlug}/members`;
    const invite = page.getByRole("button", {
      name: "Invite people",
      exact: true,
    });

    // The control: the owner's opens the dialog.
    await page.goto(members);
    await expect(invite).not.toHaveAttribute("aria-disabled");

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

    await page.goto(members);
    await expect(
      page.getByRole("row", { name: member.nickname }),
    ).toContainText("Member");
    await expect(invite).toHaveAttribute("aria-disabled", "true");
    await expect(invite).toHaveAccessibleDescription(
      "Inviting people needs org:invite, and no policy grants it to you.",
    );
  });

  for (const theme of ["light", "dark"] as const) {
    test(`the list, its invitation and a person's page pass axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);
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
        `/console/orgs/${signedIn.orgSlug}/members/${signedIn.userSlug}`,
      );
      await expect(
        page.getByRole("region", { name: "Your access" }),
      ).toBeVisible();
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
