import type { Locator, Page } from "@playwright/test";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember } from "./seed";

/** The seeded group's page: its GRN's identifier is `engineering`. */
function teamUrl(orgSlug: string, team = "engineering"): string {
  return `/console/orgs/${orgSlug}/teams/${team}`;
}

/**
 * Clicks a control that only works once React has hydrated it. A click
 * taken before then is dropped, not queued, so it is repeated until
 * what it opens is there.
 */
async function clickUntil(control: Locator, opened: Locator): Promise<void> {
  await expect(async () => {
    await control.click();
    await expect(opened).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 15_000 });
}

/**
 * The members-table row naming `text`. Substring on purpose: a row's
 * accessible name is all of its cells, and `text` is one of them.
 */
function memberRow(page: Page, text: string): Locator {
  return page.getByRole("row", { name: text });
}

/**
 * Adds the option named `name` through the team page's form. An
 * option's name is the member's name and then its identifier.
 */
async function addMember(page: Page, name: string): Promise<void> {
  const option = page.getByRole("option", { name, exact: true });
  await clickUntil(
    page.getByRole("combobox", { name: "Person or agent", exact: true }),
    option,
  );
  await option.click();
  await page.getByRole("button", { name: "Add to team", exact: true }).click();
}

/** Opens the Settings tab of the team page on screen. */
async function openSettings(page: Page): Promise<void> {
  await clickUntil(
    page.getByRole("tab", { name: "Settings", exact: true }),
    page.getByLabel("Team name", { exact: true }),
  );
}

test.describe("teams", () => {
  test("opens a team from the list and comes back to the list", async ({
    page,
    signedIn,
  }) => {
    const nav = page.getByRole("navigation", {
      name: "Organization sections",
      exact: true,
    });
    const breadcrumb = page.getByRole("navigation", {
      name: "breadcrumb",
      exact: true,
    });
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);

    await nav.getByRole("link", { name: "Teams", exact: true }).click();
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);
    await page
      .getByRole("link", { name: signedIn.groupName, exact: true })
      .click();

    await expect(page).toHaveURL(teamUrl(signedIn.orgSlug));
    await expect(
      page.getByRole("heading", {
        level: 1,
        name: signedIn.groupName,
        exact: true,
      }),
    ).toBeVisible();
    // Substring on purpose: the breadcrumb is a trail, and the section
    // and the team are two items in it.
    await expect(breadcrumb).toContainText("Teams");
    await expect(breadcrumb).toContainText(signedIn.groupName);
    await expect(
      nav.getByRole("link", { name: "Teams", exact: true }),
    ).toHaveAttribute("aria-current", "location");

    await page.goBack();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);
    await expect(
      page.getByRole("link", { name: signedIn.groupName, exact: true }),
    ).toBeVisible();

    await breadcrumb
      .getByRole("link", { name: signedIn.orgName, exact: true })
      .click();
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);
  });

  test("holds a person and an agent, and removing one touches nothing else", async ({
    page,
    signedIn,
  }) => {
    const person = `${signedIn.nickname} ${signedIn.userSlug}`;
    const agent = `Unnamed agent ${signedIn.agentIdentifier}`;

    // A second team, created through the console, holding the agent.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams`);
    await page.getByRole("link", { name: "Create team", exact: true }).click();
    await page.getByLabel("Team name", { exact: true }).fill("Operations");
    await page
      .getByRole("button", { name: "Create team", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Operations", exact: true }),
    ).toBeVisible();
    await expect(page).toHaveURL(
      new RegExp(`/console/orgs/${signedIn.orgSlug}/teams/[0-9a-f]+$`),
    );
    const operations = page.url();
    await addMember(page, agent);
    await expect(memberRow(page, signedIn.agentIdentifier)).toContainText(
      "Agent",
    );

    // The seeded team takes both types.
    await page.goto(teamUrl(signedIn.orgSlug));
    await expect(
      page.getByText("This team has no members.", { exact: true }),
    ).toBeVisible();
    await addMember(page, person);
    await expect(memberRow(page, signedIn.nickname)).toContainText("Person");
    await addMember(page, agent);
    await expect(memberRow(page, signedIn.agentIdentifier)).toContainText(
      "Agent",
    );
    // Neither can be chosen again: the seed holds one of each.
    await expect(
      page.getByText(
        "Every person and agent of this organization is already in this team.",
        { exact: true },
      ),
    ).toBeVisible();

    await page.reload();
    await expect(memberRow(page, signedIn.nickname)).toContainText("Person");

    // Removing the agent from this team leaves its other membership.
    // The sentence names the agent without its identifier, so its row
    // is what says which member the control removes.
    const remove = memberRow(page, signedIn.agentIdentifier).getByRole(
      "button",
      {
        name: `Remove an unnamed agent from ${signedIn.groupName}`,
        exact: true,
      },
    );
    const confirm = page.getByRole("button", {
      name: "Remove from team",
      exact: true,
    });
    await clickUntil(remove, confirm);
    await expect(
      page.getByRole("alertdialog", {
        name: `Remove an unnamed agent from ${signedIn.groupName}?`,
        exact: true,
      }),
    ).toBeVisible();
    await confirm.click();
    await expect(memberRow(page, signedIn.agentIdentifier)).toHaveCount(0);
    await expect(memberRow(page, signedIn.nickname)).toContainText("Person");

    await page.goto(operations);
    await expect(memberRow(page, signedIn.agentIdentifier)).toContainText(
      "Agent",
    );
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    await expect(
      page.getByText(signedIn.agentIdentifier, { exact: true }),
    ).toBeVisible();
  });

  test("shows a failed addition and keeps the page usable", async ({
    page,
    browser,
    baseURL,
    signedIn,
  }) => {
    const person = `${signedIn.nickname} ${signedIn.userSlug}`;
    await page.goto(teamUrl(signedIn.orgSlug));

    // Another tab adds the same person first, so this page's choice is
    // stale when it is submitted and garam refuses it.
    const other = await browser.newContext({
      baseURL,
      storageState: await page.context().storageState(),
    });
    const otherPage = await other.newPage();
    await otherPage.goto(teamUrl(signedIn.orgSlug));
    await addMember(otherPage, person);
    await expect(memberRow(otherPage, signedIn.nickname)).toBeVisible();
    await other.close();

    await addMember(page, person);

    await expect(
      page.getByText("Already a member of this team. The list now shows it.", {
        exact: true,
      }),
    ).toBeVisible();
    // The refusal is the choice's error.
    await expect(
      page.getByRole("combobox", { name: "Person or agent", exact: true }),
    ).toHaveAttribute("aria-invalid", "true");
    // Recovered: the refusal leaves one membership, which the page
    // shows without a reload.
    await expect(memberRow(page, signedIn.nickname)).toHaveCount(1);
  });

  test("keeps a member nobody can resolve visible and removable", async ({
    page,
    signedIn,
  }) => {
    // The API takes any GRN as a member, which is how a membership can
    // outlive whoever it named.
    const added = await page.request.post(
      `/api/orgs/${signedIn.orgSlug}/groups/engineering/members`,
      { data: { memberGrn: "grn:root:default:user:departed" } },
    );
    expect(added.status()).toBe(204);

    await page.goto(teamUrl(signedIn.orgSlug));
    const row = memberRow(page, "departed");
    await expect(row).toContainText("Not found in this organization");
    await expect(row).toContainText("Person");

    // A pattern on purpose: the button's name goes on to name the
    // member and the team, and the row already fixes which member.
    const remove = row.getByRole("button", { name: /^Remove / });
    const confirm = page.getByRole("button", {
      name: "Remove from team",
      exact: true,
    });
    await clickUntil(remove, confirm);
    await confirm.click();

    await expect(
      page.getByText("This team has no members.", { exact: true }),
    ).toBeVisible();
  });

  test("renames a team and deletes it after naming it", async ({
    page,
    signedIn,
  }) => {
    await page.goto(teamUrl(signedIn.orgSlug));
    await openSettings(page);
    await page.getByLabel("Team name", { exact: true }).fill("Platform");
    await page.getByRole("button", { name: "Save name", exact: true }).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Platform", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Renamed to Platform.", { exact: true }),
    ).toBeVisible();

    const dialog = page.getByRole("alertdialog", {
      name: "Delete Platform?",
      exact: true,
    });
    await page
      .getByRole("region", { name: "Danger zone", exact: true })
      .getByRole("button", { name: "Delete team", exact: true })
      .click();
    await expect(dialog).toBeVisible();
    await dialog
      .getByRole("button", { name: "Delete team", exact: true })
      .click();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);
    await expect(
      page.getByText("This organization has no teams.", { exact: true }),
    ).toBeVisible();
    await page.goto(teamUrl(signedIn.orgSlug));
    await expect(
      page.getByRole("heading", { name: "Platform", exact: true }),
    ).toHaveCount(0);
  });

  test("sends the old Groups and Members addresses to Teams and People", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/groups`);
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);

    await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/people`);
    // Presence: the heading carries the list's count after the title.
    await expect(
      page.getByRole("heading", { level: 1, name: /^People/ }),
    ).toBeVisible();
  });

  test("offers a member none of the writes the server denies them", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    const teams = `/console/orgs/${signedIn.orgSlug}/teams`;

    // The control: the owner is offered each write on the same pages.
    await page.goto(teams);
    await expect(
      page.getByRole("link", { name: "Create team", exact: true }),
    ).toBeVisible();
    await page.goto(teamUrl(signedIn.orgSlug));
    await expect(
      page.getByRole("combobox", { name: "Person or agent", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("tab", { name: "Settings", exact: true }),
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

    // The list reads for a member, so the page rendered and the action
    // is absent rather than unreached.
    await page.goto(teams);
    await expect(
      page.getByRole("link", { name: signedIn.groupName, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Create team", exact: true }),
    ).toBeHidden();

    await page.goto(teamUrl(signedIn.orgSlug));
    await expect(
      page.getByRole("heading", {
        level: 1,
        name: signedIn.groupName,
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("tab", { name: "Overview", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("combobox", { name: "Person or agent", exact: true }),
    ).toBeHidden();
    await expect(
      page.getByRole("tab", { name: "Settings", exact: true }),
    ).toBeHidden();

    await page.goto(`${teams}/new`);
    await expect(
      page.getByText("You cannot create teams here", { exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel("Team name", { exact: true })).toBeHidden();
  });

  for (const theme of ["light", "dark"] as const) {
    test(`the Teams routes pass axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);

      await page.goto(`/console/orgs/${signedIn.orgSlug}/teams`);
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);

      await page.goto(`/console/orgs/${signedIn.orgSlug}/teams/new`);
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);

      await page.goto(teamUrl(signedIn.orgSlug));
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);

      await openSettings(page);
      expect(await axeViolations(page)).toEqual([]);

      const dialog = page.getByRole("alertdialog");
      await page
        .getByRole("region", { name: "Danger zone", exact: true })
        .getByRole("button", { name: "Delete team", exact: true })
        .click();
      await expect(dialog).toBeVisible();
      expect(await axeViolations(page, ['[role="alertdialog"]'])).toEqual([]);
    });
  }
});
