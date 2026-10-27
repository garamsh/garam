import type { Page } from "@playwright/test";

import { axeViolations, expectHydrated } from "./axe";

/**
 * The budget of the two flows that walk a whole operator lifecycle,
 * with six axe scans each and, in the first, up to one 5 s re-read of
 * the setup page. Measured with the scans scoped as below: 10 to 21 s
 * on one CPU, and at most 30.7 s on a host at load average 36, where
 * the 30 s default failed them (#1511). Twice that worst run.
 */
const LIFECYCLE_BUDGET_MS = 60_000;

/** The tab panel on show; inactive ones stay mounted, hidden. */
const ACTIVE_PANEL = '[role="tabpanel"][data-state="active"]';
import { expect, test } from "./fixtures";
import {
  recordedAuthority,
  seedHostedOperator,
  seedKeyExchange,
  seedMember,
} from "./seed";

/** Where the seeded hosted operator's control service is registered. */
const ENDPOINT = "https://control.example.test/garam";

/** What the stub received of one reports read. */
type ReportsRead = { path: string; authorization: string | undefined };

/**
 * Answers the reports read at the registered endpoint with `answer`
 * under the agreed CORS contract, or as a refused connection, and
 * keeps what each read carried. The control service is a stub; garam
 * is real.
 */
async function stubReports(
  page: Page,
  consoleOrigin: string,
  answer: { status: number; body: unknown } | "unreachable",
): Promise<ReportsRead[]> {
  const received: ReportsRead[] = [];
  const cors = { "access-control-allow-origin": consoleOrigin };
  await page.route(`${new URL(ENDPOINT).origin}/**`, async (route) => {
    const request = route.request();
    if (request.method() === "OPTIONS") {
      await route.fulfill({
        status: 204,
        headers: {
          ...cors,
          "access-control-allow-methods": "GET, POST, PUT",
          "access-control-allow-headers":
            "Authorization, Content-Type, Garam-Contract-Version",
        },
      });
      return;
    }
    received.push({
      path: new URL(request.url()).pathname,
      authorization: (await request.allHeaders())["authorization"],
    });
    if (answer === "unreachable") {
      await route.abort("connectionrefused");
      return;
    }
    await route.fulfill({
      status: answer.status,
      headers: cors,
      contentType: "application/json",
      body: JSON.stringify(answer.body),
    });
  });
  return received;
}

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
  test("keeps Connect an operator open when its request is answered 503, then registers on the retry (#1631)", async ({
    page,
    signedIn,
  }) => {
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    // A proxy in front of the console answers the Server Action's own
    // POST 503, as on aurora; the next attempt goes through.
    let refused = 0;
    await page.route(`**${operators}`, async (route) => {
      if (route.request().method() === "POST" && refused === 0) {
        refused += 1;
        await route.fulfill({
          status: 503,
          contentType: "text/html",
          body: "Service Unavailable",
        });
        return;
      }
      await route.continue();
    });
    await page.goto(operators);
    await expectHydrated(page);

    await page
      .getByRole("button", { name: "Connect an operator", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "Connect an operator" });
    await dialog
      .getByRole("textbox", { name: "Cluster manager identifier" })
      .fill("aurora");
    await dialog
      .getByRole("textbox", { name: "Control service identifier" })
      .fill("aurora-control");
    const register = dialog.getByRole("button", {
      name: "Register",
      exact: true,
    });
    await register.click();

    await expect(dialog.getByRole("alert")).toHaveText(
      "garam didn't answer, so the operator may or may not have been registered. Check the operator list before trying again.",
    );
    await expect(dialog).toBeVisible();
    expect(refused).toBe(1);

    // The control: the same dialog, its request let through, registers.
    await register.click();
    await expect(page).toHaveURL(`${operators}/aurora/connect`);
  });

  test("connects an Operator as one registration of its two identities, and adds a control service to an older one (#1636)", async ({
    page,
    signedIn,
  }) => {
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    const connect = async () => {
      await page.goto(operators);
      await expectHydrated(page);
      await page
        .getByRole("button", { name: "Connect an operator", exact: true })
        .click();
      return page.getByRole("dialog", { name: "Connect an operator" });
    };

    let dialog = await connect();
    // The seeded identity is a cluster manager with no control service,
    // so the dialog asks; its default is a new Operator.
    await expect(dialog.getByRole("combobox", { name: "Register" })).toHaveText(
      "A new Operator",
    );
    await dialog
      .getByRole("textbox", { name: "Cluster manager identifier" })
      .fill("aurora");
    await dialog
      .getByRole("textbox", { name: "Control service identifier" })
      .fill("aurora-control");
    await dialog.getByRole("button", { name: "Register", exact: true }).click();
    await expect(page).toHaveURL(`${operators}/aurora/connect`);
    await expect(
      page.getByRole("button", { name: "Issue both keys", exact: true }),
    ).toBeVisible();

    // The seeded Operator predates registering both together; its
    // control service joins it.
    dialog = await connect();
    await dialog.getByRole("combobox", { name: "Register" }).click();
    await page
      .getByRole("option", { name: /^The control service of / })
      .click();
    expect(await axeViolations(page)).toEqual([]);
    const control = `${signedIn.operatorIdentifier}-control`;
    await dialog
      .getByRole("textbox", { name: "Control service identifier" })
      .fill(control);
    await dialog.getByRole("button", { name: "Register", exact: true }).click();
    await expect(page).toHaveURL(`${operators}/${control}/connect`);

    await page.goto(operators);
    await expectHydrated(page);
    // The seeded Operator and aurora: two rows for four identities.
    await expect(
      page.getByRole("heading", { level: 1, name: /Operators/ }),
    ).toHaveText(/^Operators\s*2$/);
    const row = page
      .getByRole("row")
      .filter({ has: page.getByRole("link", { name: "aurora", exact: true }) });
    await expect(
      row.getByRole("link", { name: "aurora-control", exact: true }),
    ).toHaveAttribute("href", `${operators}/aurora-control`);
    await expect(
      page.getByRole("link", { name: control, exact: true }),
    ).toHaveCount(1);
    await expect(page.getByText("Not connected")).toHaveCount(0);
  });

  test("connects an Operator: registers both identities, issues both keys, places them and sees both sign in", async ({
    page,
    signedIn,
  }) => {
    test.setTimeout(LIFECYCLE_BUDGET_MS);
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    await page.goto(operators);
    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);

    await page
      .getByRole("button", { name: "Connect an operator", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "Connect an operator" });
    const identifier = dialog.getByRole("textbox", {
      name: "Cluster manager identifier",
    });
    await identifier.fill("Field-Two");
    // The rule is refused beside its field, before a round trip.
    await expect(identifier).toHaveAttribute("aria-invalid", "true");
    await expect(
      dialog.getByRole("button", { name: "Register", exact: true }),
    ).toBeDisabled();
    expect(await axeViolations(page, ['[role="dialog"]'])).toEqual([]);

    await identifier.fill("field-two");
    await dialog
      .getByRole("textbox", { name: "Control service identifier" })
      .fill("field-two-control");
    await dialog.getByRole("textbox", { name: "Name" }).fill("Field two");
    // Registering issues nothing, so it goes straight on to the setup
    // page, where both keys are issued (ADR-0117, ADR-0124).
    await dialog.getByRole("button", { name: "Register", exact: true }).click();

    await expect(page).toHaveURL(`${operators}/field-two/connect`);
    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);
    await page
      .getByRole("button", { name: "Issue both keys", exact: true })
      .click();
    const pem = await keepSecret(
      page,
      "Save the cluster manager's private key",
    );
    expect(pem).toContain("-----BEGIN PRIVATE KEY-----");
    const controlPem = await keepSecret(
      page,
      "Save the control service's private key",
    );
    expect(controlPem).toContain("-----BEGIN PRIVATE KEY-----");
    expect(controlPem).not.toBe(pem);

    await expect(
      page.getByRole("heading", { name: "Place the keys", exact: true }),
    ).toBeVisible();
    // The local stack's token URL, and each private key only as its
    // file.
    await expect(
      page
        .getByRole("region", { name: "The control service's key" })
        .getByText(/kubectl create secret generic/),
    ).toContainText("private-key.pem=./field-two-control.pem");
    const snippet = page
      .getByRole("region", { name: "The cluster manager's key" })
      .getByText(/kubectl create secret generic garam-credential/);
    await expect(snippet).toContainText(
      "token-url='https://machine.garam.localhost/oauth/token'",
    );
    await expect(snippet).toContainText("kubectl apply --server-side");
    await expect(snippet).toContainText("private-key.pem=./field-two.pem");
    await expect(snippet).not.toContainText("PRIVATE KEY");
    // The setup page was read whole when it opened; each step changes
    // its content alone (#1511).
    expect(await axeViolations(page, ["main"])).toEqual([]);

    await page.getByRole("button", { name: /placed both keys/ }).click();
    await expect(page.getByText("Waiting for its first sign-in")).toHaveCount(
      2,
    );
    // The local stack runs no machine listener, so nothing exchanges
    // the keys; the test writes what an exchange leaves, and the step
    // finds it on its own next check.
    await seedKeyExchange(signedIn, "field-two");
    await seedKeyExchange(signedIn, "field-two-control");
    await expect(page.getByText("Field two is connected")).toBeVisible({
      timeout: 15_000,
    });
    expect(await axeViolations(page, ["main"])).toEqual([]);

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
    // The dialog closes on submit, before garam has answered; the row
    // reads Revoked only once the action's response has arrived. A
    // reload before then cuts that response off (#1454).
    const revoked = keys.getByRole("row").filter({ hasText: "Revoked" });
    await expect(revoked).toHaveCount(1);
    await page.reload();
    await expect(revoked).toHaveCount(1);
  });

  test("renames, and deletes once its identifier is typed", async ({
    page,
    signedIn,
  }) => {
    test.setTimeout(LIFECYCLE_BUDGET_MS);
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;
    const seeded = `${operators}/${signedIn.operatorIdentifier}`;

    await page.goto(seeded);
    await expectHydrated(page);
    expect(await axeViolations(page)).toEqual([]);
    // The page was read whole above; a tab changes its own panel and
    // nothing around it, so each is read alone. Reading the whole page
    // again per tab was most of this test's time on a loaded host
    // (#1511).
    for (const tab of ["Keys", "Control service", "Delegation", "Settings"]) {
      await page.getByRole("tab", { name: tab, exact: true }).click();
      expect(await axeViolations(page, [ACTIVE_PANEL])).toEqual([]);
    }

    // Renaming keeps the identifier the page is addressed by.
    const name = page.getByRole("textbox", { name: "Name" });
    await name.fill("Staging cluster");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("Saved as Staging cluster")).toBeVisible();

    // An operator is deleted by its dialog once its identifier is
    // typed.
    await page.goto(operators);
    await expectHydrated(page);
    await page
      .getByRole("button", { name: "Connect an operator", exact: true })
      .click();
    const register = page.getByRole("dialog", { name: "Connect an operator" });
    await register
      .getByRole("textbox", { name: "Cluster manager identifier" })
      .fill("new");
    await register
      .getByRole("textbox", { name: "Control service identifier" })
      .fill("new-control");
    await register
      .getByRole("button", { name: "Register", exact: true })
      .click();
    // Registering opens the operator's setup page.
    await expect(page).toHaveURL(`${operators}/new/connect`);

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
    await expect(
      page.getByRole("link", { name: "new", exact: true }),
    ).toBeHidden();
    // Its control service still names it, so it stands under a cluster
    // manager that is not registered (ADR-0124).
    await expect(
      page
        .getByRole("row")
        .filter({
          has: page.getByRole("link", { name: "new-control", exact: true }),
        })
        .getByText("Not registered"),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Staging cluster", exact: true }),
    ).toBeVisible();
  });

  test("shows what each operator reports running and its last contact, read under an authority garam bound to the read", async ({
    page,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, ["operator:read"]);
    const digest = `sha256:${"ab".repeat(32)}`;
    const received = await stubReports(page, new URL(baseURL!).origin, {
      status: 200,
      body: {
        operators: [
          {
            grn: `grn:${signedIn.orgSlug}:default:operator:${signedIn.operatorIdentifier}`,
            images: [
              {
                name: "manager",
                ref: "ghcr.io/garamsh/operator:v0.4.0",
                digest,
              },
              { name: "agent", ref: "ghcr.io/garamsh/gagent:v1.2.0" },
            ],
            reportedAt: new Date(Date.now() - 3_600_000).toISOString(),
            lastContactAt: new Date(Date.now() - 60_000).toISOString(),
          },
        ],
      },
    });
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;

    await page.goto(operators);
    await expectHydrated(page);
    const table = page.getByRole("table", { name: "Operators" });
    const seeded = table
      .getByRole("row")
      .filter({ hasText: signedIn.operatorName });
    await expect(
      seeded.getByText("ghcr.io/garamsh/gagent:v1.2.0", { exact: true }),
    ).toBeVisible();
    // The control service's own identity is no controller to report.
    const hosted = table.getByRole("row").filter({ hasText: "Hosted control" });
    await expect(
      hosted.getByText("A control service, not a controller", { exact: true }),
    ).toHaveCount(2);
    expect(await axeViolations(page)).toEqual([]);

    await page.goto(`${operators}/${signedIn.operatorIdentifier}`);
    await expectHydrated(page);
    // The digest is shortened on screen and copied whole.
    await expect(
      page.getByText(`sha256:${"ab".repeat(6)}…`, { exact: true }),
    ).toBeVisible();
    await expect(page.getByText(digest, { exact: true })).toBeHidden();
    await expect(
      page.getByRole("button", {
        name: "Copy the manager image's digest",
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByText("ghcr.io/garamsh/operator:v0.4.0", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Not checked", { exact: true })).toBeHidden();
    expect(await axeViolations(page)).toEqual([]);

    // Each read carried its own authority, which garam recorded for
    // exactly that read.
    expect(received.length).toBeGreaterThanOrEqual(2);
    for (const read of received) {
      expect(read.path).toBe(`/garam/v1/orgs/${signedIn.orgSlug}/operators`);
      expect(read.authorization).toMatch(/^Garam-Operation [A-Za-z0-9_-]{43}$/);
      const recorded = await recordedAuthority(
        read.authorization!.slice("Garam-Operation ".length),
      );
      expect(recorded).toMatchObject({
        operation: "operator:read",
        actorGrn: signedIn.userGrn,
        audienceGrn: `grn:${signedIn.orgSlug}:default:operator:hosted`,
        targetGrn: `grn:root:default:org:${signedIn.orgSlug}`,
        requestTarget: read.path,
      });
    }
  });

  test("says the reports were not checked, and why, on the list and the operator's page, where the control service cannot be reached", async ({
    page,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, ["operator:read"]);
    await stubReports(page, new URL(baseURL!).origin, "unreachable");
    const unanswered = page.getByText(
      "Hosted control's control service did not answer, so what it reports of the operators it serves was not checked.",
      { exact: true },
    );
    const operators = `/console/orgs/${signedIn.orgSlug}/operators`;

    await page.goto(operators);
    await expectHydrated(page);
    await expect(unanswered).toBeVisible();
    const seeded = page
      .getByRole("table", { name: "Operators" })
      .getByRole("row")
      .filter({ hasText: signedIn.operatorName });
    await expect(seeded.getByText("Not checked", { exact: true })).toHaveCount(
      2,
    );
    expect(await axeViolations(page)).toEqual([]);

    await page.goto(`${operators}/${signedIn.operatorIdentifier}`);
    await expectHydrated(page);
    await expect(unanswered).toBeVisible();
    await expect(page.getByText("Not checked", { exact: true })).toHaveCount(2);
    await expect(page.getByText("Not reported", { exact: true })).toBeHidden();
    expect(await axeViolations(page)).toEqual([]);
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
    // Settings holds only what the member may do, which is nothing: no
    // form to rename and no danger zone. The selected tab is the
    // control that Settings is what was read.
    await expect(
      page.getByRole("tab", { name: "Settings", exact: true, selected: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Name", exact: true }),
    ).toBeHidden();
    await expect(
      page.getByRole("region", { name: "Danger zone" }),
    ).toBeHidden();
    expect(await axeViolations(page)).toEqual([]);
  });
});
