import type { Page } from "@playwright/test";

import { expect, test } from "./fixtures";

/** Fills a values editor row, waiting for the form to take input. */
async function fillRow(page: Page, row: number, key: string, value: string) {
  // Exact on purpose: the row's remove button is named "Remove value
  // N", which carries the field's own name as a substring.
  const keyField = page.getByLabel(`Key ${row}`, { exact: true });
  await expect(keyField).toBeEnabled();
  await keyField.fill(key);
  await page.getByLabel(`Value ${row}`, { exact: true }).fill(value);
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
    await fillRow(page, 1, "model", "sonnet");
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
      "sonnet",
    );
    const agentName = page.getByLabel("Name", { exact: true });
    await expect(agentName).toBeEnabled();
    await agentName.fill("Scout");
    await page.getByLabel("Operator", { exact: true }).selectOption({
      label: `${signedIn.operatorName} (${signedIn.operatorIdentifier})`,
    });
    await page.getByLabel("Value 1", { exact: true }).fill("haiku");
    await page.getByRole("button", { name: "Create agent" }).click();

    // The agent's page: requested apart from reported, awaiting an
    // operator because none has claimed it.
    await expect(
      page.getByRole("heading", { level: 1, name: "Scout", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Awaiting operator", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("haiku", { exact: true })).toBeVisible();
    const agentUrl = page.url();

    // Editing and then deleting the template reaches no agent.
    await page.goto(`${agents}/templates`);
    await page.getByRole("link", { name: "Researcher", exact: true }).click();
    const templateName = page.getByLabel("Name", { exact: true });
    await expect(templateName).toBeEnabled();
    await page.getByLabel("Value 1", { exact: true }).fill("opus");
    await page.getByRole("button", { name: "Save template" }).click();
    await expect(page.getByRole("status")).toHaveText("Saved Researcher.");
    await page.getByRole("button", { name: "Delete template" }).click();
    await page
      .getByRole("button", { name: "Delete Researcher", exact: true })
      .click();
    await expect(page.getByText("No templates", { exact: true })).toBeVisible();

    await page.goto(agentUrl);
    await expect(page.getByText("haiku", { exact: true })).toBeVisible();
    await expect(page.getByText("opus", { exact: true })).toHaveCount(0);

    // Renaming moves the name and keeps the identifier.
    const identifier = agentUrl.split("/").at(-1) ?? "";
    const rename = page.getByLabel("Name", { exact: true });
    await expect(rename).toBeEnabled();
    await rename.fill("Ranger");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Ranger.");
    await page.goto(agents);
    const card = page.getByRole("listitem").filter({ hasText: identifier });
    await expect(
      card.getByRole("link", { name: "Ranger", exact: true }),
    ).toBeVisible();
    await expect(card.getByText("Awaiting operator")).toBeVisible();

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

    // A policy can be given to it by the same name.
    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/permissions/attachments?subject=${encodeURIComponent(`agent:${identifier}`)}`,
    );
    await expect(
      page.getByText(`Ranger (${identifier}) · awaiting operator`).first(),
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
