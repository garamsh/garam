import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember } from "./seed";

test.describe("consents", () => {
  test("grants from the waiting card and withdraws through the named dialog", async ({
    page,
    signedIn,
  }) => {
    const partner = signedIn.partnerOrgSlug;
    const card = page.getByRole("group", {
      name: `Other organization ${partner}`,
    });

    await page.goto(`/console/orgs/${signedIn.orgSlug}/consents`);

    // The seeded state: the partner holds its half and this
    // organization holds none, so nothing crosses yet (ADR-0035).
    await expect(
      card.getByText("Waiting for this organization", { exact: true }),
    ).toBeVisible();
    await expect(
      card.getByRole("button", { name: "Withdraw consent" }),
    ).toBeHidden();

    // Granted from the card itself: the other organization is not
    // typed again, and the dialog names the act.
    await expectHydrated(page);
    await card.getByRole("button", { name: "Grant consent" }).click();
    const grant = page.getByRole("alertdialog", {
      name: "Grant this organization's consent?",
    });
    await grant.getByRole("button", { name: "Grant consent" }).click();
    await expect(grant).toBeHidden();

    await expect(
      card.getByText("Messages cross", { exact: true }),
    ).toBeVisible();
    await expect(
      card.getByText(
        "Both halves stand, so messages cross between the two organizations in either direction.",
        { exact: true },
      ),
    ).toBeVisible();

    await card.getByRole("button", { name: "Withdraw consent" }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Withdraw this organization's consent?",
    });
    await expect(dialog).toBeVisible();
    // Opening the dialog withdrew nothing.
    await expect(
      card.getByText("Messages cross", { exact: true }),
    ).toBeVisible();

    await dialog.getByRole("button", { name: "Withdraw consent" }).click();

    // The half this organization held is down and the partner's still
    // stands, so the relation waits where it started.
    await expect(dialog).toBeHidden();
    await expect(
      card.getByText("Waiting for this organization", { exact: true }),
    ).toBeVisible();
  });

  test("keeps the identifier typed when a grant is refused", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/consents`);
    await expectHydrated(page);

    await page.getByRole("button", { name: "New consent" }).click();
    const dialog = page.getByRole("dialog", { name: "Grant consent" });
    const field = dialog.getByLabel("Other organization", { exact: true });
    // An organization consents with another, never with itself, so
    // garam refuses this one (ADR-0035).
    await field.fill(signedIn.orgSlug);
    await dialog.getByRole("button", { name: "Grant consent" }).click();

    await expect(field).toHaveAttribute("aria-invalid", "true");
    // The grant's own sentence, not garam's English.
    await expect(field).toHaveAccessibleDescription(
      "The identifier the other organization is addressed by. That is this organization's own identifier. A consent is granted to another organization.",
    );
    await expect(field).toHaveValue(signedIn.orgSlug);
    await expect(dialog).toBeVisible();
  });

  test("says a member is not shown the relations and offers no grant", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
    const consents = `/console/orgs/${signedIn.orgSlug}/consents`;
    // The control: the owner reads the relations and may grant.
    await page.goto(consents);
    await expect(
      page.getByRole("button", { name: "New consent", exact: true }),
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
    await page.goto(consents);

    await expect(
      page.getByText("Consents not shown", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "New consent", exact: true }),
    ).toBeHidden();
  });

  for (const theme of ["light", "dark"] as const) {
    test(`passes axe in the ${theme} theme`, async ({ page, signedIn }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}/consents`);
      await expect(
        page.getByRole("heading", { level: 1, name: /^Consents/ }),
      ).toBeVisible();

      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
