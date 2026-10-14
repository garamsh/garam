import type { Page } from "@playwright/test";

import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";
import { seedDefinition, seedKeyExchange, seedMember } from "./seed";

/** Takes the key a SecretReveal is showing, and closes it as kept. */
async function keepSecret(page: Page, title: string): Promise<string> {
  const reveal = page.getByRole("alertdialog", { name: title });
  await expect(reveal).toBeVisible();
  const secret = (await reveal.locator("pre").textContent()) ?? "";
  await reveal.getByRole("checkbox").check();
  await reveal.getByRole("button", { name: "Done", exact: true }).click();
  await expect(reveal).toBeHidden();
  return secret;
}

test.describe("operators", () => {
  test("connects an operator: registers it, issues its key, places it and sees it sign in", async ({
    page,
    signedIn,
  }) => {
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    await page.goto(operators);
    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);

    await page
      .getByRole("button", { name: "Connect an operator", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "Connect an operator" });
    const identifier = dialog.getByRole("textbox", { name: "Identifier" });
    await identifier.fill("Field-Two");
    // The rule is refused beside its field, before a round trip.
    await expect(identifier).toHaveAttribute("aria-invalid", "true");
    await expect(
      dialog.getByRole("button", { name: "Register", exact: true }),
    ).toBeDisabled();
    expect(await axeViolations(page, ['[role="dialog"]'])).toEqual([]);

    await identifier.fill("field-two");
    await dialog.getByRole("textbox", { name: "Name" }).fill("Field two");
    await dialog.getByRole("button", { name: "Register", exact: true }).click();
    // The dialog is named for what it says now: the registration.
    const registered = page.getByRole("dialog", {
      name: "Registered Field two",
    });
    await expect(
      registered.getByText("The operator is registered"),
    ).toBeVisible();
    // The legacy secrets wait behind a closed disclosure.
    await expect(
      registered.getByRole("button", {
        name: "Legacy credential and enrollment token",
      }),
    ).toHaveAttribute("aria-expanded", "false");
    await registered
      .getByRole("link", { name: /Continue: issue a key/ })
      .click();

    await expect(page).toHaveURL(`${operators}/field-two/connect`);
    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);
    await page.getByRole("button", { name: "Issue key", exact: true }).click();
    const pem = await keepSecret(page, "Save the private key");
    expect(pem).toContain("-----BEGIN PRIVATE KEY-----");

    await expect(
      page.getByRole("heading", { name: "Place the key", exact: true }),
    ).toBeVisible();
    // The local stack's token URL, and the private key only as a file.
    const snippet = page.getByText(
      /kubectl create secret generic garam-credential/,
    );
    await expect(snippet).toContainText(
      "token-url=https://machine.garam.localhost/oauth/token",
    );
    await expect(snippet).toContainText("private-key.pem=./field-two.pem");
    await expect(snippet).not.toContainText("PRIVATE KEY");
    expect(await axeViolations(page)).toEqual([]);

    await page.getByRole("button", { name: /placed the key/ }).click();
    await expect(page.getByText("Waiting for its first sign-in")).toBeVisible();
    // The local stack runs no machine listener, so nothing exchanges the
    // key; the test writes what an exchange leaves, and the step finds
    // it on its own next check.
    await seedKeyExchange(signedIn, "field-two");
    await expect(page.getByText("Field two is connected")).toBeVisible({
      timeout: 15_000,
    });
    expect(await axeViolations(page)).toEqual([]);

    await page.getByRole("link", { name: /Go to Field two/ }).click();
    await expect(page).toHaveURL(`${operators}/field-two?tab=keys`);
    const keys = page.getByRole("table", { name: "Keys" });
    await expect(keys.getByText("Active", { exact: true })).toBeVisible();
    await expect(keys.getByText("Never", { exact: true })).toBeHidden();
    expect(await axeViolations(page)).toEqual([]);
  });

  test("rotates a key beside the one in use and revokes the old one", async ({
    page,
    signedIn,
  }) => {
    const operator = `/console/orgs/${signedIn.orgSlug}/operators/${signedIn.operatorIdentifier}`;
    await page.goto(`${operator}/connect`);
    await expectHydrated(page);
    await page.getByRole("button", { name: "Issue key", exact: true }).click();
    await keepSecret(page, "Save the private key");

    await page.goto(`${operator}?tab=keys`);
    await expectHydrated(page);
    await page.getByRole("link", { name: "Rotate key", exact: true }).click();
    await expect(page.getByRole("radio", { name: /Rotate/ })).toBeChecked();
    await page
      .getByRole("button", { name: "Issue the new key", exact: true })
      .click();
    await keepSecret(page, "Save the private key");

    await page.goto(`${operator}?tab=keys`);
    await expectHydrated(page);
    const keys = page.getByRole("table", { name: "Keys" });
    const retiring = keys.getByRole("row").filter({ hasText: "Retiring" });
    await expect(retiring).toHaveCount(1);
    await expect(
      keys.getByRole("row").filter({ hasText: "Active" }),
    ).toHaveCount(1);

    await retiring.getByRole("button", { name: "Revoke", exact: true }).click();
    const confirm = page.getByRole("alertdialog", { name: "Revoke this key?" });
    expect(await axeViolations(page, ['[role="alertdialog"]'])).toEqual([]);
    await confirm
      .getByRole("button", { name: "Revoke key", exact: true })
      .click();
    await page.reload();
    await expect(
      page
        .getByRole("table", { name: "Keys" })
        .getByRole("row")
        .filter({ hasText: "Revoked" }),
    ).toHaveCount(1);
  });

  test("renames, re-issues the legacy credential, and deletes only what no definition names", async ({
    page,
    signedIn,
  }) => {
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    const seeded = `${operators}/${signedIn.operatorIdentifier}`;

    await page.goto(seeded);
    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);
    for (const tab of ["Keys", "Control service", "Delegation", "Settings"]) {
      await page.getByRole("tab", { name: tab, exact: true }).click();
      expect(await axeViolations(page)).toEqual([]);
    }

    // Renaming keeps the identifier the page is addressed by.
    const name = page.getByRole("textbox", { name: "Name" });
    await name.fill("Staging cluster");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("Saved as Staging cluster")).toBeVisible();

    // Re-issuing the legacy credential asks first, then shows it once.
    await page
      .getByRole("button", { name: "Re-issue credential", exact: true })
      .click();
    await page
      .getByRole("alertdialog")
      .getByRole("button", { name: "Re-issue credential", exact: true })
      .click();
    const credential = await keepSecret(page, "Save the new credential");
    expect(credential).toMatch(/^garam_oc_[0-9a-f]{64}$/);

    // A definition naming the operator refuses its delete, and the
    // dialog names it rather than offering the confirmation.
    await seedDefinition(signedIn, "Batch runner");
    await page.goto(`${seeded}?tab=settings`);
    await expectHydrated(page);
    await page
      .getByRole("button", { name: "Delete operator", exact: true })
      .click();
    const blocked = page.getByRole("alertdialog");
    await expect(
      blocked.getByRole("link", { name: "Batch runner", exact: true }),
    ).toBeVisible();
    await expect(
      blocked.getByRole("button", { name: "Delete operator", exact: true }),
    ).toBeDisabled();
    await blocked.getByRole("button", { name: "Cancel" }).click();

    // The control: an operator no definition names is deleted by the
    // same dialog once its identifier is typed.
    await page.goto(operators);
    await expectHydrated(page);
    await page
      .getByRole("button", { name: "Connect an operator", exact: true })
      .click();
    const register = page.getByRole("dialog", { name: "Connect an operator" });
    await register.getByRole("textbox", { name: "Identifier" }).fill("new");
    await register
      .getByRole("button", { name: "Register", exact: true })
      .click();
    await expect(
      register.getByText("The operator is registered"),
    ).toBeVisible();

    // `new` is a valid identifier, so it is an operator's page and no
    // create route takes it (ADR-0074).
    await page.goto(`${operators}/new?tab=settings`);
    await expectHydrated(page);
    await expect(
      page.getByRole("heading", { level: 1, name: "new", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Delete operator", exact: true })
      .click();
    const dialog = page.getByRole("alertdialog");
    expect(await axeViolations(page, ['[role="alertdialog"]'])).toEqual([]);
    await dialog.getByLabel(/to confirm/).fill("new");
    await dialog
      .getByRole("button", { name: "Delete operator", exact: true })
      .click();
    await expect(page).toHaveURL(operators);
    await expect(page.getByText("new", { exact: true })).toBeHidden();
    await expect(
      page.getByRole("link", { name: "Staging cluster", exact: true }),
    ).toBeVisible();
  });

  test("offers a member, whom the server denies changing operators, the reads alone", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;

    // The control: the owner is offered connecting on the same page.
    await page.goto(operators);
    await expect(
      page.getByRole("button", { name: "Connect an operator", exact: true }),
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

    await page.goto(operators);
    await expect(
      page.getByRole("link", { name: signedIn.operatorName, exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Connect an operator", exact: true }),
    ).toBeHidden();

    await page.goto(`${operators}/${signedIn.operatorIdentifier}?tab=settings`);
    await expect(
      page.getByText("You can view this operator but not change it."),
    ).toBeVisible();
    await expect(
      page.getByRole("region", { name: "Danger zone" }),
    ).toBeHidden();
    expect(await axeViolations(page)).toEqual([]);
  });
});
