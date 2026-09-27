import { expect, test } from "./fixtures";

test.describe("agent definitions", () => {
  test("defines an agent and lists it under the organization", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    await expect(
      page.getByText("This organization has defined no agents.", {
        exact: true,
      }),
    ).toBeVisible();

    // The operator is chosen by name, with the identifier beside it
    // (ADR-0074). The field is disabled until React hydrates, so
    // waiting for it to be enabled waits on the form being able to
    // hold the choice.
    const operator = page.getByLabel("Operator");
    await expect(operator).toBeEnabled();
    await operator.selectOption({
      label: `${signedIn.operatorName} (${signedIn.operatorIdentifier})`,
    });
    await page.getByLabel("Key 1", { exact: true }).fill("model");
    // Exact on purpose: the row's remove button is named "Remove value
    // 1", which carries the field's own name as a substring.
    await page.getByLabel("Value 1", { exact: true }).fill("haiku");
    await page.getByRole("button", { name: "Add value" }).click();
    await page.getByLabel("Key 2", { exact: true }).fill("system-prompt");
    await page.getByLabel("Value 2", { exact: true }).fill("be brief");
    await page.getByRole("button", { name: "Define agent" }).click();

    const minted = page.getByLabel("Agent identifier");
    await expect(minted).toBeVisible();
    const identifier = await minted.inputValue();
    // What garam minted is a GRN's last segment and never a GRN
    // (ADR-0011), so it carries no colon.
    expect(identifier).toMatch(/^[a-z0-9-]+$/);

    await page.reload();

    await expect(page.getByText(identifier, { exact: true })).toBeVisible();
    await expect(
      page.getByRole("link", { name: signedIn.operatorName, exact: true }),
    ).toBeVisible();
    await expect(page.getByText("be brief", { exact: true })).toBeVisible();
  });
});

test.describe("agents", () => {
  test("shows an agent and the states its two parties reported", async ({
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
