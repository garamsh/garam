import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember } from "./seed";

test.describe("organization overview", () => {
  test("leads with what is failing, linked to its section", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);

    // The seeded agent reports failed and disconnected, each its own
    // state (ADR-0053), and the seeded operator's enrollment expired.
    // Inside main: the router's announcer is an alert too.
    const banner = page.getByRole("main").getByRole("alert");
    await expect(banner).toContainText("3 states need attention");
    await banner.getByRole("link", { name: "1 failed in Agents" }).click();

    await expect(page).toHaveURL(`/console/orgs/${signedIn.orgSlug}/agents`);
  });

  test("counts consents and permissions for the owner and not for a member", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
    const overview = `/console/orgs/${signedIn.orgSlug}`;
    const main = page.getByRole("main");
    await page.goto(overview);
    // Substring on purpose: the card's name is its label and its count.
    await expect(main.getByRole("link", { name: /^Consents\b/ })).toContainText(
      "1",
    );
    await expect(
      main.getByText("Not counted: the server did not let you read them."),
    ).toHaveCount(0);

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
    await page.goto(overview);

    await expect(main.getByRole("link", { name: /^Consents\b/ })).toContainText(
      "Not counted",
    );
    await expect(
      main.getByRole("link", { name: /^Permissions\b/ }),
    ).toContainText("Not counted");
    // The control: what every member reads is still counted.
    await expect(main.getByRole("link", { name: /^Teams\b/ })).toContainText(
      "1",
    );
  });

  for (const theme of ["light", "dark"] as const) {
    test(`passes axe in the ${theme} theme`, async ({ page, signedIn }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}`);
      await expect(page.getByRole("main").getByRole("alert")).toBeVisible();

      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
