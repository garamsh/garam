import { randomBytes } from "node:crypto";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedHosts, seedMember } from "./seed";

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

  test("keeps a member's approve, withdraw and revoke on the page, inactive, and sends garam nothing", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
    const hosts = await seedHosts(signedIn);
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
    const approvals = page.getByRole("table", { name: "Approvals" });
    const pending = approvals.getByRole("row", {
      name: new RegExp(hosts.pending),
    });
    const supervisor = page
      .getByRole("table", { name: "Supervisors" })
      .getByRole("row", { name: new RegExp(hosts.active) });
    await expect(pending).toContainText("Waiting for the host");
    await expect(supervisor).toContainText("Active");

    // The permission check answers agent:enroll from the check every
    // approval, withdrawal and revoke runs, which denies a member.
    const writes: string[] = [];
    page.on("request", (request) => {
      if (request.method() !== "GET") writes.push(request.url());
    });
    for (const control of [
      page.getByRole("button", { name: "Approve a host" }),
      pending.getByRole("button", { name: "Withdraw" }),
      supervisor.getByRole("button", { name: "Revoke" }),
    ]) {
      await expect(control).toHaveAttribute("aria-disabled", "true");
      await expect(control).toHaveAccessibleDescription(
        "Approving, withdrawing and revoking hosts needs agent:enroll, and no policy grants it to you.",
      );
      // Playwright refuses an aria-disabled control unless forced, so
      // forcing is what shows its click does nothing.
      await control.click({ force: true });
    }
    await expect(page.getByRole("alertdialog")).toHaveCount(0);
    expect(writes).toEqual([]);
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
