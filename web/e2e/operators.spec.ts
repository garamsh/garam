import { expect, test } from "./fixtures";

test.describe("operators", () => {
  test("registers an operator and defines an agent against it", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/operators`);
    await expect(
      page.getByText(signedIn.operatorIdentifier, { exact: true }),
    ).toBeVisible();

    // The refusal is the form's own, so it appears only once the form
    // is interactive: a fill taken before React hydrates is dropped,
    // not queued. Waiting on the refusal waits on exactly that, and on
    // the way past it the spec sees the character rule enforced without
    // a round trip. It is located by its text rather than by its
    // `alert` role, which Next.js's route announcer also carries.
    const characterRule = page.getByText(
      "An operator identifier holds only lowercase letters, digits and hyphens.",
      { exact: true },
    );
    await expect(async () => {
      await page.getByLabel("Identifier").fill("Field-Two");
      await expect(characterRule).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });
    await expect(
      page.getByRole("button", { name: "Register operator" }),
    ).toBeDisabled();

    await page.getByLabel("Identifier").fill("field-two");
    await page.getByRole("button", { name: "Register operator" }).click();
    await expect(
      page.getByRole("heading", { name: "Registered field-two", exact: true }),
    ).toBeVisible();

    // The token the operator enrols with is shown once, beside the
    // sentence saying so (ADR-0063). Substring, because the sentence is
    // longer than the claim under test.
    const token = page.getByLabel("Enrollment token");
    await expect(token).toHaveValue(/^[0-9a-f]{48}$/);
    await expect(
      page.getByText("This is the only time this token is shown"),
    ).toBeVisible();

    // A reload is what a person who navigated away does, and it is
    // where the token stops existing: the record survives it and the
    // token does not.
    await page.reload();
    await expect(page.getByText("field-two", { exact: true })).toBeVisible();
    await expect(token).toBeHidden();

    // An operator this organization has not registered is refused where
    // the identifier can still be corrected, and the refusal names it.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    await expect(async () => {
      await page.getByLabel("Operator").fill("Gagent-Dev");
      await expect(characterRule).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });

    await page.getByLabel("Operator").fill("gagent-dev");
    await page.getByRole("button", { name: "Define agent" }).click();
    await expect(
      page.getByText(
        "this organization has registered no operator named gagent-dev",
        { exact: true },
      ),
    ).toBeVisible();

    // The operator registered above is the control: the same form, the
    // same values, and the definition lands.
    await page.getByLabel("Operator").fill("field-two");
    await page.getByLabel("Key 1", { exact: true }).fill("model");
    // Exact on purpose: the row's remove button is named "Remove value
    // 1", which carries the field's own name as a substring.
    await page.getByLabel("Value 1", { exact: true }).fill("haiku");
    await page.getByRole("button", { name: "Define agent" }).click();

    const minted = page.getByLabel("Agent identifier");
    await expect(minted).toBeVisible();
    const identifier = await minted.inputValue();

    await page.reload();
    await expect(page.getByText(identifier, { exact: true })).toBeVisible();
    await expect(page.getByText("field-two", { exact: true })).toBeVisible();
  });
});
