import type { BrowserContext } from "@playwright/test";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, holdSession, test } from "./fixtures";
import { seedMember } from "./seed";

/** Signs the browser in as `token`'s session at the run's origin. */
async function signInAs(
  context: BrowserContext,
  baseURL: string | undefined,
  token: string,
) {
  await context.clearCookies();
  await holdSession(context, baseURL, token);
}

test.describe("Home", () => {
  test("opens with a sentence linking what needs a look", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);

    // The seeded agent reports failed and disconnected, and is in no
    // team the owner is in, since the owner is in none.
    const lede = page
      .getByRole("main")
      .getByText(/^Nothing is waiting on you\./);
    await expect(lede).toHaveText(
      `Nothing is waiting on you. You are in no team here. ${signedIn.agentIdentifier} needs a look.`,
    );
    await lede.getByRole("link", { name: signedIn.agentIdentifier }).click();

    await expect(page).toHaveURL(
      `/console/orgs/${signedIn.orgSlug}/agents/${signedIn.agentIdentifier}`,
    );
  });

  test("leaves out of a member's sidebar what no policy lets them read, and says why", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
    const member = await seedMember(signedIn);
    await signInAs(context, baseURL, member.sessionToken);
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);

    const nav = page.getByRole("navigation", { name: "Organization sections" });
    await expect(
      nav.getByText("Permissions hidden: no policy lets you read it.", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      nav.getByRole("link", { name: "Permissions", exact: true }),
    ).toHaveCount(0);
    // The control: what the member role reads is still listed.
    await expect(
      nav.getByRole("link", { name: "Teams", exact: true }),
    ).toBeVisible();
  });

  test("lists the owner's sections in full", async ({ page, signedIn }) => {
    // The control for the member above: the same page, an owner.
    await page.goto(`/console/orgs/${signedIn.orgSlug}`);
    const nav = page.getByRole("navigation", { name: "Organization sections" });

    await expect(
      nav.getByRole("link", { name: "Permissions", exact: true }),
    ).toBeVisible();
    await expect(nav.getByText(/hidden: no policy/)).toHaveCount(0);
  });

  for (const theme of ["light", "dark"] as const) {
    test(`passes axe in the ${theme} theme`, async ({ page, signedIn }) => {
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}`);
      await expect(
        page.getByRole("region", { name: /^Worth a look/ }),
      ).toBeVisible();

      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });

    test(`passes axe for a member, note included, in the ${theme} theme`, async ({
      page,
      context,
      baseURL,
      signedIn,
    }) => {
      const member = await seedMember(signedIn);
      await signInAs(context, baseURL, member.sessionToken);
      await useTheme(page, theme);
      await page.goto(`/console/orgs/${signedIn.orgSlug}`);
      const nav = page.getByRole("navigation", {
        name: "Organization sections",
      });
      await expect(nav.getByText(/hidden: no policy/)).toBeVisible();

      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});

test.describe("command palette", () => {
  test("finds an agent by name and opens its page", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/consents`);
    await expectHydrated(page);

    await page.keyboard.press("ControlOrMeta+k");
    const palette = page.getByRole("dialog", { name: "Search or jump to" });
    await palette.getByRole("combobox").fill(signedIn.agentIdentifier);
    await palette
      .getByRole("option", { name: new RegExp(signedIn.agentIdentifier) })
      .click();

    await expect(page).toHaveURL(
      `/console/orgs/${signedIn.orgSlug}/agents/${signedIn.agentIdentifier}`,
    );
  });

  test("opens on . with the page's actions, and one opens its dialog", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams/engineering`);
    await expectHydrated(page);

    await page.keyboard.press(".");
    const palette = page.getByRole("dialog", {
      name: "Actions on this page",
    });
    await expect(palette).toContainText(
      "Removes the team and its memberships, for good; its members stay.",
    );
    await palette.getByRole("option", { name: /^Delete team/ }).click();

    await expect(
      page.getByRole("alertdialog", {
        name: `Delete ${signedIn.groupName}?`,
      }),
    ).toBeVisible();
  });
});
