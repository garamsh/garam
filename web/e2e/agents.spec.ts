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

    // The refusal is the form's own, so it appears only once the form
    // is interactive: a fill taken before React hydrates is dropped,
    // not queued. Waiting on the refusal waits on exactly that, and on
    // the way past it the spec sees the constraint enforced without a
    // round trip. It is located by its text rather than by its `alert`
    // role, which Next.js's route announcer also carries.
    const refusal = page.getByText(
      "An operator identifier holds only lowercase letters, digits and hyphens.",
      { exact: true },
    );
    await expect(async () => {
      await page.getByLabel("Operator").fill("Field-One");
      await expect(refusal).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });
    await expect(
      page.getByRole("button", { name: "Define agent" }),
    ).toBeDisabled();

    await page.getByLabel("Operator").fill(signedIn.operatorIdentifier);
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
      page.getByText(signedIn.operatorIdentifier, { exact: true }),
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
