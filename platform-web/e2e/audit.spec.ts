import type { BrowserContext } from "@playwright/test";

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

test.describe("audit", () => {
  test("shows the owner a policy write, and a member that the read is refused", async ({
    page,
    context,
    baseURL,
    signedIn,
  }) => {
    const created = await page.request.post(
      `/api/orgs/${signedIn.orgSlug}/iam-policies`,
      {
        data: {
          name: "Deny agent activation",
          body: {
            kind: "guardrail",
            text: 'forbid (principal, action == Iam::Action::"agent.activate", resource);',
          },
        },
      },
    );
    expect(created.status()).toBe(201);

    await page.goto(`/console/orgs/${signedIn.orgSlug}/audit`);

    const write = page.getByRole("row", { name: /iam:policy\.create/ }).first();
    await expect(write).toContainText("created");
    await expect(write).toContainText("v1");
    await expect(
      page.getByText("You can't view this organization's audit records"),
    ).toHaveCount(0);

    // The member's read is the same request, refused by the service.
    const member = await seedMember(signedIn);
    await signInAs(context, baseURL, member.sessionToken);
    await page.goto(`/console/orgs/${signedIn.orgSlug}/audit`);

    await expect(
      page.getByText("You can't view this organization's audit records"),
    ).toBeVisible();
    await expect(page.getByText("No audit records")).toHaveCount(0);
  });
});
