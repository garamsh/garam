import type { Browser, Page } from "@playwright/test";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { CONSOLE_URL, expect, holdSession, test } from "./fixtures";
import { seedSignedInUser } from "./seed";

/**
 * Creates a member invitation as the page's owner and returns its path,
 * the console's: this site sends it there (ADR-0131).
 */
async function invite(page: Page, orgSlug: string): Promise<string> {
  const response = await page.request.post(`/api/orgs/${orgSlug}/invitations`, {
    data: { role: "member" },
  });
  expect(response.status()).toBe(201);
  const { token } = (await response.json()) as { token: string };
  return `/console/invitations/${token}`;
}

/** A page in a fresh context, holding the given session or none. */
async function visitor(
  browser: Browser,
  baseURL: string,
  session?: string,
): Promise<Page> {
  const context = await browser.newContext({ baseURL });
  if (session !== undefined) await holdSession(context, baseURL, session);
  return context.newPage();
}

// The states an invitation puts the way in and the invitation page in,
// in both themes: usable, then spent once the invitee has joined. The
// way in is this site's and the invitation page the console's, reached
// from here by the address the invitation link carries.
test.describe("invitation pages", () => {
  for (const theme of ["light", "dark"] as const) {
    test(`pass axe while usable and once spent, in the ${theme} theme`, async ({
      page,
      browser,
      baseURL,
      signedIn,
    }) => {
      const invitation = await invite(page, signedIn.orgSlug);
      const signIn = `/login?next=${encodeURIComponent(invitation)}`;

      const usable = await visitor(browser, baseURL!);
      await useTheme(usable, theme);
      await usable.goto(signIn);
      await expect(
        usable.getByRole("heading", {
          level: 1,
          name: `Sign in to join ${signedIn.orgName}`,
        }),
      ).toBeVisible();
      expect(await axeViolations(usable)).toEqual([]);

      const invitee = await seedSignedInUser();
      const joining = await visitor(browser, baseURL!, invitee.sessionToken);
      await useTheme(joining, theme);
      await joining.goto(invitation);
      await expect(
        joining.getByRole("heading", {
          level: 1,
          name: `Join ${signedIn.orgName}`,
        }),
      ).toBeVisible();
      await expectHydrated(joining);
      expect(await axeViolations(joining)).toEqual([]);
      await joining
        .getByRole("button", { name: `Join ${signedIn.orgName}` })
        .click();
      await expect(joining).toHaveURL(
        `${CONSOLE_URL}/console/orgs/${signedIn.orgSlug}`,
      );

      await joining.goto(invitation);
      await expect(
        joining.getByRole("heading", {
          level: 1,
          name: "This invitation has already been used",
        }),
      ).toBeVisible();
      await expectHydrated(joining);
      expect(await axeViolations(joining)).toEqual([]);

      const spent = await visitor(browser, baseURL!);
      await useTheme(spent, theme);
      await spent.goto(signIn);
      await expect(
        spent
          .getByRole("status")
          .getByText("This invitation has already been used"),
      ).toBeVisible();
      expect(await axeViolations(spent)).toEqual([]);
    });
  }
});
