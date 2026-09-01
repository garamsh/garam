import { expect, test } from "./fixtures";

test.describe("consents", () => {
  test("grants this organization's half of a seeded pair and withdraws it again", async ({
    page,
    signedIn,
  }) => {
    const partner = signedIn.partnerOrgSlug;
    const waiting = page.getByText(
      `${partner} has granted its half. Messages stay refused until this organization grants its own.`,
      { exact: true },
    );
    const crossing = page.getByText(
      `Both halves stand, so messages cross between this organization and ${partner} in either direction.`,
      { exact: true },
    );

    await page.goto(`/console/orgs/${signedIn.orgSlug}/consents`);

    // The seeded state: the partner holds its half and this
    // organization holds none, so nothing crosses yet (ADR-0035).
    await expect(page.getByText(partner, { exact: true })).toBeVisible();
    await expect(page.getByText("Waiting", { exact: true })).toBeVisible();
    await expect(waiting).toBeVisible();
    await expect(page.getByRole("button", { name: "Withdraw" })).toBeHidden();

    await page.getByLabel("Other organization", { exact: true }).fill(partner);
    await page.getByRole("button", { name: "Grant consent" }).click();

    await expect(crossing).toBeVisible();
    await expect(
      page.getByText("Messages cross", { exact: true }),
    ).toBeVisible();

    // Withdrawing asks first, and the ask is the form's own — so it
    // appears only once the row is interactive. Waiting on the
    // confirmation waits on exactly that.
    const confirm = page.getByRole("button", { name: "Confirm withdrawal" });
    await expect(async () => {
      await page.getByRole("button", { name: "Withdraw" }).click();
      await expect(confirm).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });
    // The click that asked for the confirmation withdrew nothing.
    await expect(crossing).toBeVisible();

    await confirm.click();

    // The half this organization held is down and the partner's still
    // stands, so the relation waits where it started.
    await expect(waiting).toBeVisible();
    await expect(crossing).toBeHidden();
  });
});
