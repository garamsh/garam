import type { Page } from "@playwright/test";

import { expect, test } from "./fixtures";
import { seedCutoverDefinition, type Seed } from "./seed";

/** Fills a values editor row, waiting for the form to take input. */
async function fillRow(page: Page, row: number, key: string, value: string) {
  // Exact on purpose: the row's remove button is named "Remove value
  // N", which carries the field's own name as a substring.
  const keyField = page.getByLabel(`Key ${row}`, { exact: true });
  await expect(keyField).toBeEnabled();
  await keyField.fill(key);
  await page.getByLabel(`Value ${row}`, { exact: true }).fill(value);
}

/**
 * Defines an agent from a blank start through the console, as a person
 * does, and answers with its identifier. No operator claims it.
 */
async function defineAgent(
  page: Page,
  signedIn: Seed,
  name: string,
): Promise<string> {
  await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/new`);
  const agentName = page.getByLabel("Name", { exact: true });
  await expect(agentName).toBeEnabled();
  await agentName.fill(name);
  await page.getByLabel("Operator", { exact: true }).selectOption({
    label: `${signedIn.operatorName} (${signedIn.operatorIdentifier})`,
  });
  await page.getByRole("button", { name: "Create agent" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name, exact: true }),
  ).toBeVisible();
  return page.url().split("/").at(-1) ?? "";
}

test.describe("agent templates", () => {
  test("an agent copies a template once and keeps the copy", async ({
    page,
    signedIn,
  }) => {
    const agents = `/console/orgs/${signedIn.orgSlug}/agents`;

    // A template, created inside Agents.
    await page.goto(`${agents}/templates`);
    await expect(page.getByText("No templates", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "New template", exact: true }).click();
    const name = page.getByLabel("Name", { exact: true });
    await expect(name).toBeEnabled();
    await name.fill("Researcher");
    await page.getByLabel("Description", { exact: true }).fill("Reads papers");
    await fillRow(page, 1, "workspace", "papers");
    await page.getByRole("button", { name: "Create template" }).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Researcher", exact: true }),
    ).toBeVisible();

    // An agent from it: the copied value is edited before creating.
    await page
      .getByRole("link", { name: "Create agent from template", exact: true })
      .click();
    await expect(
      page.getByText(
        "Copied from Researcher. Changing the template later does not change this agent.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(page.getByLabel("Value 1", { exact: true })).toHaveValue(
      "papers",
    );
    const agentName = page.getByLabel("Name", { exact: true });
    await expect(agentName).toBeEnabled();
    await agentName.fill("Scout");
    await page.getByLabel("Operator", { exact: true }).selectOption({
      label: `${signedIn.operatorName} (${signedIn.operatorIdentifier})`,
    });
    await page.getByLabel("Value 1", { exact: true }).fill("reports");
    await page.getByRole("button", { name: "Create agent" }).click();

    // The agent's page: requested apart from reported, awaiting an
    // operator because none has claimed it.
    await expect(
      page.getByRole("heading", { level: 1, name: "Scout", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Awaiting operator", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("reports", { exact: true })).toBeVisible();
    const agentUrl = page.url();

    // Editing and then deleting the template reaches no agent.
    await page.goto(`${agents}/templates`);
    await page.getByRole("link", { name: "Researcher", exact: true }).click();
    const templateName = page.getByLabel("Name", { exact: true });
    await expect(templateName).toBeEnabled();
    await page.getByLabel("Value 1", { exact: true }).fill("archive");
    await page.getByRole("button", { name: "Save template" }).click();
    await expect(page.getByRole("status")).toHaveText("Saved Researcher.");
    await page.getByRole("button", { name: "Delete template" }).click();
    await page
      .getByRole("button", { name: "Delete Researcher", exact: true })
      .click();
    await expect(page.getByText("No templates", { exact: true })).toBeVisible();

    await page.goto(agentUrl);
    await expect(page.getByText("reports", { exact: true })).toBeVisible();
    await expect(page.getByText("archive", { exact: true })).toHaveCount(0);
  });
});

test.describe("named agents", () => {
  test("renaming moves the name and keeps the identifier", async ({
    page,
    signedIn,
  }) => {
    const identifier = await defineAgent(page, signedIn, "Scout");

    const rename = page.getByLabel("Name", { exact: true });
    await expect(rename).toBeEnabled();
    await rename.fill("Ranger");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Ranger.");
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    const card = page.getByRole("listitem").filter({ hasText: identifier });
    await expect(
      card.getByRole("link", { name: "Ranger", exact: true }),
    ).toBeVisible();
    await expect(card.getByText("Awaiting operator")).toBeVisible();
  });

  test("a team and a policy take a defined agent by name before any claim", async ({
    page,
    signedIn,
  }) => {
    const identifier = await defineAgent(page, signedIn, "Ranger");

    // A team takes the agent by name before any operator claims it: the
    // membership is its GRN, minted with the definition.
    const team = `/console/orgs/${signedIn.orgSlug}/teams/engineering`;
    await page.goto(team);
    const choice = page.getByLabel("Person or agent", { exact: true });
    await expect(choice).toBeEnabled();
    await choice.selectOption({
      label: `Ranger (${identifier}) · awaiting operator · Agent`,
    });
    await page
      .getByRole("button", { name: "Add to team", exact: true })
      .click();
    const row = page.getByRole("row", { name: /Ranger/ });
    await expect(row).toContainText("Agent");
    await page.reload();
    await expect(page.getByRole("row", { name: /Ranger/ })).toContainText(
      "Agent",
    );

    // A policy can be given to it by the same name: its policies are
    // shown under that name.
    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/permissions/attachments?subject=${encodeURIComponent(`agent:${identifier}`)}`,
    );
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: `Ranger (${identifier}) · awaiting operator · Agent`,
        exact: true,
      }),
    ).toBeVisible();
  });
});

test.describe("agents", () => {
  test("shows a reported agent and the states its two parties reported", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);

    await expect(
      page.getByText(signedIn.agentIdentifier, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.agentProvisioningState, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.agentRuntimeState, { exact: true }),
    ).toBeVisible();
  });
});

test.describe("cutover", () => {
  test("a definition cutting over or cut over is marked archival, and only its name changes", async ({
    page,
    signedIn,
  }) => {
    const switched = await seedCutoverDefinition(signedIn, "Moved", "switched");
    const frozen = await seedCutoverDefinition(signedIn, "Moving", "frozen");
    // The control: an attempt rolled back is none open, so its
    // definition reads as a legacy one does.
    const legacy = await seedCutoverDefinition(
      signedIn,
      "Stayed",
      "rolled_back",
    );
    const agents = `/console/orgs/${signedIn.orgSlug}/agents`;
    const operator = `/console/orgs/${signedIn.orgSlug}/operators/${signedIn.operatorIdentifier}`;

    // Where definitions are listed: the roster and the operator's page.
    for (const [href, row] of [
      [
        agents,
        (id: string) => page.getByRole("listitem").filter({ hasText: id }),
      ],
      [
        operator,
        (id: string) =>
          page
            .locator("section")
            .filter({
              has: page.getByRole("heading", { name: "Agent definitions" }),
            })
            .getByRole("listitem")
            .filter({ hasText: id }),
      ],
    ] as const) {
      await page.goto(href);
      await expect(
        row(switched).getByText("Managed by the Operator", { exact: true }),
      ).toBeVisible();
      await expect(
        row(frozen).getByText("Cut-over in progress", { exact: true }),
      ).toBeVisible();
      await expect(row(legacy)).toBeVisible();
      await expect(
        row(legacy).getByText("Managed by the Operator"),
      ).toHaveCount(0);
      await expect(row(legacy).getByText("Cut-over in progress")).toHaveCount(
        0,
      );
    }

    const archival =
      "These settings are archival and read-only in garam; only the name still changes here.";
    await page.goto(`${agents}/${frozen}`);
    await expect(
      page.getByText(`Cut-over in progress. ${archival}`, { exact: true }),
    ).toBeVisible();

    await page.goto(`${agents}/${legacy}`);
    await expect(
      page.getByText("Saved values; application is not reported.", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.getByText(archival)).toHaveCount(0);

    // A switched definition's values are archival, and its name is the
    // one thing garam still changes (ADR-0086).
    await page.goto(`${agents}/${switched}`);
    await expect(
      page.getByText(`Managed by the Operator. ${archival}`, { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("haiku", { exact: true })).toBeVisible();
    const rename = page.getByLabel("Name", { exact: true });
    await expect(rename).toBeEnabled();
    await rename.fill("Moved on");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Moved on.");
  });
});
