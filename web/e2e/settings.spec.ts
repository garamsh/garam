import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember } from "./seed";

test.describe("organization settings", () => {
  test("renames the organization, and the shell names it the new way", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/settings`);
    await expectHydrated(page);
    const general = page.getByRole("region", { name: "General" });
    await general.getByLabel("Name", { exact: true }).fill("Renamed Org");
    await general.getByRole("button", { name: "Save", exact: true }).click();

    await expect(general.getByRole("status")).toHaveText(
      "Saved as Renamed Org",
    );
    // The breadcrumb reads the organization from the shell, which the
    // rename revalidated.
    await expect(
      page
        .getByRole("navigation", { name: "breadcrumb" })
        .getByRole("link", { name: "Renamed Org", exact: true }),
    ).toBeVisible();
  });

  test("deletes the organization once garam's count is shown and its name typed", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/settings`);
    await expectHydrated(page);
    await page
      .getByRole("region", { name: "Danger zone" })
      .getByRole("button", { name: "Delete organization" })
      .click();
    const dialog = page.getByRole("alertdialog", {
      name: `Delete ${signedIn.orgName}?`,
    });
    // The seed holds one team and one project, which garam counted.
    await expect(dialog.getByRole("listitem")).toContainText([
      "1team",
      "1project",
    ]);
    const confirm = dialog.getByRole("button", { name: "Delete organization" });
    await expect(confirm).toBeDisabled();
    await dialog
      .getByLabel(`Type ${signedIn.orgName} to confirm`)
      .fill(signedIn.orgName);
    await confirm.click();

    await expect(page).toHaveURL(/\/console\/orgs$/);
    await page.goto(`/console/orgs/${signedIn.orgSlug}/settings`);
    await expect(
      page.getByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeVisible();
  });

  test("offers a member neither rename nor delete, and still names the organization", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
    const settings = `/console/orgs/${signedIn.orgSlug}/settings`;
    // The control: the owner is offered both on the same page.
    await page.goto(settings);
    await expect(
      page.getByRole("button", { name: "Delete organization" }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "Save" })).toBeVisible();

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
    await page.goto(settings);

    const general = page.getByRole("region", { name: "General" });
    await expect(
      general.getByText(signedIn.orgName, { exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "Save" })).toBeHidden();
    await expect(
      page.getByRole("region", { name: "Danger zone" }),
    ).toBeHidden();
  });

  for (const theme of ["light", "dark"] as const) {
    test(`passes axe in the ${theme} theme, with the delete dialog open`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}/settings`);
      await expect(
        page.getByRole("heading", { level: 1, name: "Settings" }),
      ).toBeVisible();
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);

      await page.getByRole("button", { name: "Delete organization" }).click();
      await expect(page.getByRole("alertdialog")).toBeVisible();
      expect(await axeViolations(page, ['[role="alertdialog"]'])).toEqual([]);
    });
  }
});
