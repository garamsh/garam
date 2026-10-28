import type { APIRequestContext } from "@playwright/test";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { SITE_URL, expect, test } from "./fixtures";

const DEVICE_GRANT = "urn:ietf:params:oauth:grant-type:device_code";

/** What `garam login` asks for: a device code and its user code. */
async function startLogin(request: APIRequestContext) {
  const response = await request.post("/api/auth/device", {
    form: { client_id: "garam-cli", client_name: "e2e-laptop" },
  });
  expect(response.status()).toBe(200);
  return (await response.json()) as {
    device_code: string;
    user_code: string;
    verification_uri: string;
    verification_uri_complete: string;
    interval: number;
  };
}

/** One poll of the token endpoint, as `garam login` makes it. */
function poll(request: APIRequestContext, deviceCode: string) {
  return request.post("/api/auth/device/token", {
    form: {
      grant_type: DEVICE_GRANT,
      device_code: deviceCode,
      client_id: "garam-cli",
    },
  });
}

test.describe("device sign-in", () => {
  test("signs a CLI in once the person approves its code on the page", async ({
    page,
    playwright,
    baseURL,
    signedIn,
  }) => {
    // The CLI holds no cookie: its requests go through a context of
    // their own.
    const cli = await playwright.request.newContext({ baseURL });
    const login = await startLogin(cli);
    expect(login.verification_uri).toBe("/device");

    // The control: undecided, the poll is told to wait.
    const pending = await poll(cli, login.device_code);
    expect(pending.status()).toBe(400);
    expect(await pending.json()).toMatchObject({
      error: "authorization_pending",
    });

    await page.goto(login.verification_uri_complete);
    await expect(page).toHaveURL(
      `/console/device?code=${encodeURIComponent(login.user_code)}`,
    );
    await expect(
      page.getByText(`This lets the CLI act as ${signedIn.nickname}.`),
    ).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Code" })).toHaveValue(
      login.user_code,
    );
    await expectHydrated(page);
    await page.getByRole("button", { name: "Approve", exact: true }).click();
    await expect(page.getByText("The CLI is signed in")).toBeVisible();

    // The CLI polls at the interval it was given; the first poll after
    // the approval that the interval admits redeems the code.
    let token = "";
    await expect(async () => {
      const answer = await poll(cli, login.device_code);
      expect(answer.status()).toBe(200);
      token = ((await answer.json()) as { access_token: string }).access_token;
    }).toPass({
      intervals: [login.interval * 1000],
      timeout: (login.interval + 10) * 1000 * 2,
    });

    // The bearer is the person who approved.
    const me = await cli.get("/api/me", {
      headers: { authorization: `Bearer ${token}` },
    });
    expect(me.status()).toBe(200);
    expect(await me.json()).toMatchObject({ nickname: signedIn.nickname });

    // The person sees the CLI among their sessions and revokes it.
    await page.goto("/console/sessions");
    await expectHydrated(page);
    await page
      .getByRole("button", { name: "Revoke CLI on e2e-laptop", exact: true })
      .click();
    const dialog = page.getByRole("alertdialog", {
      name: "Revoke CLI on e2e-laptop?",
    });
    await dialog
      .getByRole("button", { name: "Revoke session", exact: true })
      .click();
    // The dialog closes once garam has revoked it, and the list read
    // again holds no such row.
    await expect(dialog).toBeHidden();
    await expect(
      page
        .getByRole("table", { name: "Sessions" })
        .getByText("CLI on e2e-laptop"),
    ).toHaveCount(0);
    const revoked = await cli.get("/api/me", {
      headers: { authorization: `Bearer ${token}` },
    });
    expect(revoked.status()).toBe(401);
    await cli.dispose();
  });

  test("a denied code signs no CLI in", async ({
    page,
    playwright,
    baseURL,
    signedIn,
  }) => {
    void signedIn;
    const cli = await playwright.request.newContext({ baseURL });
    const login = await startLogin(cli);

    await page.goto(`/device?user_code=${login.user_code}`);
    await expectHydrated(page);
    await page.getByRole("button", { name: "Deny", exact: true }).click();
    await expect(page.getByText("Sign-in denied")).toBeVisible();

    await expect(async () => {
      const answer = await poll(cli, login.device_code);
      expect(await answer.json()).toMatchObject({ error: "access_denied" });
    }).toPass({
      intervals: [login.interval * 1000],
      timeout: (login.interval + 10) * 1000 * 2,
    });
    await cli.dispose();
  });

  test("sends a signed-out visitor to sign in and back with the code", async ({
    page,
  }) => {
    await page.goto("/device?code=WDJB-MJHT");

    await expect(page).toHaveURL(
      `${SITE_URL}/login?next=${encodeURIComponent("/console/device?code=WDJB-MJHT")}`,
    );
  });

  for (const theme of ["light", "dark"] as const) {
    test(`the device and sessions pages pass axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      void signedIn;
      await useTheme(page, theme);
      await page.goto("/console/device?code=WDJB-MJHT");
      await expect(
        page.getByRole("heading", { level: 1, name: "Sign in the CLI" }),
      ).toBeVisible();
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);

      await page.goto("/console/sessions");
      await expect(
        page.getByRole("heading", { level: 1, name: /^Sessions/ }),
      ).toBeVisible();
      await expectHydrated(page);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
