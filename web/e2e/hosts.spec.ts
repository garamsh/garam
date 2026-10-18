import { randomBytes } from "node:crypto";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember } from "./seed";

/** What `garam standalone prepare` prints for a host, made up here. */
function prepared(): string {
  return JSON.stringify({
    requestId: `e2e-${randomBytes(6).toString("hex")}`,
    secretSha256: randomBytes(32).toString("hex"),
  });
}

test.describe("hosts", () => {
  test("approves a host from what prepare printed, then withdraws the approval", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/hosts`);
    await expectHydrated(page);

    await page.getByRole("button", { name: "Approve a host" }).click();
    const dialog = page.getByRole("alertdialog", { name: "Approve a host" });
    expect(await axeViolations(page, ['[role="alertdialog"]'])).toEqual([]);
    await dialog
      .getByRole("textbox", { name: "What garam standalone prepare printed" })
      .fill(prepared());
    await dialog.getByRole("textbox", { name: "Host name" }).fill("build-09");
    await dialog.getByRole("button", { name: "Approve host" }).click();
    await expect(page.getByText("Approved build-09")).toBeVisible();

    const approvals = page.getByRole("table", { name: "Approvals" });
    const row = approvals.getByRole("row", { name: /build-09/ });
    await expect(row).toContainText("Waiting for the host");

    await row
      .getByRole("button", { name: "Withdraw the approval for build-09" })
      .click();
    await page
      .getByRole("alertdialog")
      .getByRole("button", { name: "Withdraw approval" })
      .click();
    await expect(row).toContainText("Withdrawn");
  });

  test("offers a member approving, which garam decides on current policy and refuses", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
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

    await page.goto(`/console/orgs/${signedIn.orgSlug}/hosts`);
    await expectHydrated(page);

    // The permission check answers agent:enroll as unknown, since the
    // current policy decides it, so the control is offered and the
    // write refused (ADR-0103).
    await page.getByRole("button", { name: "Approve a host" }).click();
    const dialog = page.getByRole("alertdialog", { name: "Approve a host" });
    await dialog
      .getByRole("textbox", { name: "What garam standalone prepare printed" })
      .fill(prepared());
    await dialog.getByRole("textbox", { name: "Host name" }).fill("build-10");
    await dialog.getByRole("button", { name: "Approve host" }).click();

    await expect(dialog.getByRole("alert")).toContainText(
      "You don't have permission to do this.",
    );
  });

  for (const theme of ["light", "dark"] as const) {
    test(`passes axe in the ${theme} theme`, async ({ page, signedIn }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}/hosts`);
      await expect(
        page.getByRole("heading", { level: 1, name: "Hosts" }),
      ).toBeVisible();
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});

test.describe("an agent's host", () => {
  test("keeps Replace host on the page, inactive, while nothing holds the agent", async ({
    page,
    signedIn,
  }) => {
    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/agents/${signedIn.agentIdentifier}`,
    );

    const replace = page.getByRole("button", { name: "Replace host" });
    await expect(replace).toHaveAttribute("aria-disabled", "true");
    await expect(replace).toHaveAccessibleDescription(
      "Nothing holds it yet, so there is no host to replace.",
    );
  });
});
