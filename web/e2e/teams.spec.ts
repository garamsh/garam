import type { Locator, Page } from "@playwright/test";

import { expect, test } from "./fixtures";

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

/** Adds the option labelled `label` through the team page's form. */
async function addMember(page: Page, label: string): Promise<void> {
  await page.getByLabel("Person or agent").selectOption({ label });
  await page.getByRole("button", { name: "Add to team" }).click();
}

test.describe("teams", () => {
  test("opens a team from the list and comes back to the list", async ({
    page,
    signedIn,
  }) => {
    const nav = page.getByRole("list", { name: "Organization sections" });
    const breadcrumb = page.getByRole("navigation", { name: "breadcrumb" });
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);

    await nav.getByRole("link", { name: "Teams" }).click();
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);
    await page.getByRole("link", { name: signedIn.groupName }).click();

    await expect(page).toHaveURL(teamUrl(signedIn.orgSlug));
    await expect(
      page.getByRole("heading", { level: 1, name: signedIn.groupName }),
    ).toBeVisible();
    // Substring on purpose: the breadcrumb is a trail, and the section
    // and the team are two items in it.
    await expect(breadcrumb).toContainText("Teams");
    await expect(breadcrumb).toContainText(signedIn.groupName);
    await expect(nav.getByRole("link", { name: "Teams" })).toHaveAttribute(
      "aria-current",
      "location",
    );

    await page.goBack();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);
    await expect(
      page.getByRole("link", { name: signedIn.groupName }),
    ).toBeVisible();

    await breadcrumb.getByRole("link", { name: signedIn.orgName }).click();
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}`);
  });

  test("holds a person and an agent, and removing one touches nothing else", async ({
    page,
    signedIn,
  }) => {
    const person = `${signedIn.nickname} · Person`;
    const agent = `${signedIn.agentIdentifier} · Agent`;

    // A second team, created through the console, holding the agent.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams`);
    await page.getByRole("link", { name: "Create team" }).click();
    await page.getByLabel("Team name").fill("Operations");
    await page.getByRole("button", { name: "Create team" }).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Operations" }),
    ).toBeVisible();
    await expect(page).toHaveURL(
      new RegExp(`/console/orgs/${signedIn.orgSlug}/teams/[0-9a-f]+$`),
    );
    const operations = page.url();
    await addMember(page, agent);
    await expect(
      page.getByRole("row", { name: signedIn.agentIdentifier }),
    ).toContainText("Agent");

    // The seeded team takes both types.
    await page.goto(teamUrl(signedIn.orgSlug));
    await expect(page.getByText("This team has no members.")).toBeVisible();
    await addMember(page, person);
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toContainText("Person");
    await addMember(page, agent);
    await expect(
      page.getByRole("row", { name: signedIn.agentIdentifier }),
    ).toContainText("Agent");
    // Neither can be chosen again.
    await expect(
      page.getByLabel("Person or agent").getByRole("option", { name: person }),
    ).toHaveCount(0);

    await page.reload();
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toContainText("Person");

    // Removing the agent from this team leaves its other membership.
    const remove = page.getByRole("button", {
      name: `Remove ${signedIn.agentIdentifier} from ${signedIn.groupName}`,
    });
    const confirm = page.getByRole("button", { name: "Confirm removal" });
    await clickUntil(remove, confirm);
    await expect(
      page.getByText(
        `Remove ${signedIn.agentIdentifier} from ${signedIn.groupName}?`,
      ),
    ).toBeVisible();
    await confirm.click();
    await expect(
      page.getByRole("row", { name: signedIn.agentIdentifier }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toContainText("Person");

    await page.goto(operations);
    await expect(
      page.getByRole("row", { name: signedIn.agentIdentifier }),
    ).toContainText("Agent");
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
    const person = `${signedIn.nickname} · Person`;
    await page.goto(teamUrl(signedIn.orgSlug));
    await expect(
      page.getByLabel("Person or agent").getByRole("option", { name: person }),
    ).toHaveCount(1);

    // Another tab adds the same person first, so this page's choice is
    // stale when it is submitted and garam refuses it.
    const other = await browser.newContext({
      baseURL,
      storageState: await page.context().storageState(),
    });
    const otherPage = await other.newPage();
    await otherPage.goto(teamUrl(signedIn.orgSlug));
    await addMember(otherPage, person);
    await expect(
      otherPage.getByRole("row", { name: signedIn.nickname }),
    ).toBeVisible();
    await other.close();

    await addMember(page, person);

    await expect(
      page.getByText("Already a member of this team. The list now shows it.", {
        exact: true,
      }),
    ).toBeVisible();
    // Recovered: the refusal leaves one membership, which the page
    // shows without a reload.
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toHaveCount(1);
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
    const row = page.getByRole("row", { name: "departed" });
    await expect(row).toContainText("Not found in this organization");
    await expect(row).toContainText("Person");

    const remove = row.getByRole("button", { name: /^Remove / });
    const confirm = page.getByRole("button", { name: "Confirm removal" });
    await clickUntil(remove, confirm);
    await confirm.click();

    await expect(page.getByText("This team has no members.")).toBeVisible();
  });

  test("renames a team and deletes it after naming it", async ({
    page,
    signedIn,
  }) => {
    await page.goto(teamUrl(signedIn.orgSlug));
    await page.getByLabel("Team name").fill("Platform");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Platform" }),
    ).toBeVisible();

    const confirm = page.getByRole("button", { name: "Delete Platform" });
    await clickUntil(
      page.getByRole("button", { name: "Delete team" }),
      confirm,
    );
    await confirm.click();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);
    await expect(
      page.getByText("This organization has no teams."),
    ).toBeVisible();
    await page.goto(teamUrl(signedIn.orgSlug));
    await expect(page.getByRole("heading", { name: "Platform" })).toHaveCount(
      0,
    );
  });

  test("sends the old Groups and Members addresses to Teams and People", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/groups`);
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/teams`);

    await page.goto(`/console/orgs/${signedIn.orgSlug}/members`);
    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/people`);
    await expect(
      page.getByRole("heading", { level: 1, name: "People" }),
    ).toBeVisible();
  });
});
