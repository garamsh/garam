import type { Page } from "@playwright/test";

import { expect, test } from "./fixtures";
import { seedOperatorReport, type Seed } from "./seed";

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
    await expect(page.getByLabel("Value 1", { exact: true })).toHaveValue(
      "reports",
    );
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
    await expect(page.getByLabel("Value 1", { exact: true })).toHaveValue(
      "reports",
    );
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

test.describe("agent settings", () => {
  test("saves a revision and recovers a save another editor landed first", async ({
    page,
    context,
    signedIn,
  }) => {
    await defineAgent(page, signedIn, "Scout");
    const agentUrl = page.url();

    // A save is the next revision and says nothing was applied.
    await fillRow(page, 1, "model", "sonnet");
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(page.getByRole("status")).toHaveText("Saved as revision 2.");

    // A second editor reads revision 2; the first saves revision 3.
    const other = await context.newPage();
    await other.goto(agentUrl);
    await expect(other.getByLabel("Value 1", { exact: true })).toBeEnabled();
    await page.getByLabel("Value 1", { exact: true }).fill("opus");
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(page.getByRole("status")).toHaveText("Saved as revision 3.");

    // The second editor's save is refused and keeps what was typed;
    // keeping the edits saves them over revision 3 on purpose.
    await other.getByLabel("Value 1", { exact: true }).fill("haiku");
    await other.getByRole("button", { name: "Save settings" }).click();
    await expect(other.getByRole("alert")).toContainText(
      "Someone saved revision 3 first. Your edits are still here.",
    );
    await expect(other.getByLabel("Value 1", { exact: true })).toHaveValue(
      "haiku",
    );
    await other.getByRole("button", { name: "Keep my edits" }).click();
    await other.getByRole("button", { name: "Save settings" }).click();
    await expect(other.getByRole("status")).toHaveText("Saved as revision 4.");
    await other.close();
  });

  test("shows what the operator reported of the saved revision", async ({
    page,
    signedIn,
  }) => {
    const identifier = await defineAgent(page, signedIn, "Scout");

    // Claimed and ready, with no revision reported: unknown.
    await seedOperatorReport(signedIn, identifier, 1, null);
    await page.reload();
    await expect(page.getByText("ready", { exact: true })).toBeVisible();
    await expect(page.getByText("Unknown", { exact: true })).toBeVisible();
    await expect(page.getByText("Applied", { exact: true })).toHaveCount(0);

    // The accepted control: the saved revision, applied whole by the
    // current assignment, reads Applied.
    await seedOperatorReport(signedIn, identifier, 1, {
      epoch: 1,
      revision: 1,
      outcome: "applied",
      ignoredKeys: [],
    });
    await page.reload();
    await expect(page.getByText("Applied", { exact: true })).toBeVisible();

    await seedOperatorReport(signedIn, identifier, 1, {
      epoch: 1,
      revision: 1,
      outcome: "rejected",
      reason: "image pull failed",
    });
    await page.reload();
    await expect(page.getByText("Rejected", { exact: true })).toBeVisible();
    await expect(
      page.getByText("The operator reports: image pull failed", {
        exact: true,
      }),
    ).toBeVisible();

    // A reassignment leaves the replaced operator's report unknown.
    await seedOperatorReport(signedIn, identifier, 2, {
      epoch: 1,
      revision: 1,
      outcome: "applied",
      ignoredKeys: [],
    });
    await page.reload();
    await expect(
      page.getByText("The last report came from a replaced operator.", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.getByText("Applied", { exact: true })).toHaveCount(0);
  });
});
