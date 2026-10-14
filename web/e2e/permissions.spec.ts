import type { APIRequestContext, Locator, Page } from "@playwright/test";
import { z } from "zod";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedMember, type Seed } from "./seed";

/**
 * Clicks a control that only works once React has hydrated it. A click
 * taken before then is dropped, not queued, so it is repeated until
 * what it opens is there.
 */
async function clickUntil(control: Locator, opened: Locator): Promise<void> {
  await expect(async () => {
    await control.click();
    await expect(opened).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 15_000 });
}

/** A document denying actions across the seeded organization. */
function denyDocument(orgSlug: string, actions: string[]): string {
  return JSON.stringify({
    Version: "2026-07-25",
    Statement: [
      { Effect: "Deny", Action: actions, Resource: [`grn:${orgSlug}:*`] },
    ],
  });
}

/** The seeded team's GRN: its identifier is `engineering`. */
function teamGrn(seed: Seed): string {
  return `grn:${seed.orgSlug}:default:group:engineering`;
}

/**
 * Creates a policy through the API the console calls, for a test whose
 * subject is what happens after creation. Answers its identifier.
 */
async function createPolicy(
  request: APIRequestContext,
  seed: Seed,
  name: string,
  document: string,
): Promise<string> {
  const created = await request.post(`/api/orgs/${seed.orgSlug}/policies`, {
    data: { name, document: JSON.parse(document) },
  });
  expect(created.status()).toBe(201);
  const { grn } = z.object({ grn: z.string() }).parse(await created.json());
  return grn.split(":")[4];
}

/** Attaches a policy to the seeded team and activates it, through the API. */
async function attachToTeamAndActivate(
  request: APIRequestContext,
  seed: Seed,
  policy: string,
): Promise<void> {
  const base = `/api/orgs/${seed.orgSlug}/policies/${policy}`;
  const attached = await request.post(`${base}/attachments`, {
    data: { subjectGrn: teamGrn(seed) },
  });
  expect(attached.status()).toBe(201);
  expect((await request.post(`${base}/activate`)).status()).toBe(200);
}

function policyUrl(seed: Seed, policy: string): string {
  return `/console/orgs/${seed.orgSlug}/permissions/${policy}`;
}

function heading(page: Page, name: string): Locator {
  return page.getByRole("heading", { level: 1, name, exact: true });
}

/**
 * Replaces the JSON view's whole text. CodeMirror is a content-editable
 * region, not a field, so it is selected and typed over as a person
 * would; it takes input once it has loaded.
 */
async function replaceJson(page: Page, text: string): Promise<void> {
  await page.getByRole("tab", { name: "JSON", exact: true }).click();
  const json = page.getByRole("textbox", { name: "Document JSON" });
  await json.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(text);
}

test.describe("permissions", () => {
  test("creates a draft, attaches it to a team and sees it deny a member", async ({
    page,
    signedIn,
  }) => {
    // The seeded person joins the seeded team, so a policy attached to
    // the team reaches them through it.
    const joined = await page.request.post(
      `/api/orgs/${signedIn.orgSlug}/groups/engineering/members`,
      { data: { memberGrn: signedIn.userGrn } },
    );
    expect(joined.status()).toBe(204);

    await page.goto(`/console/orgs/${signedIn.orgSlug}/permissions`);
    await expect(
      page.getByText("This organization has no policies.", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("link", { name: "Create policy", exact: true })
      .click();
    await expect(heading(page, "New policy")).toBeVisible();

    // A document the engine refuses leaves what was typed in place, and
    // the refusal attaches to the editor.
    const name = page.getByLabel("Policy name", { exact: true });
    await expect(name).toBeEnabled();
    await name.fill("Deny project renames");
    await replaceJson(
      page,
      JSON.stringify({
        Version: "2026-07-25",
        Statement: [
          { Effect: "Maybe", Action: ["project:delete"], Resource: ["*"] },
        ],
      }),
    );
    const create = page.getByRole("button", {
      name: "Create draft",
      exact: true,
    });
    await create.click();
    const editor = page.getByRole("group", { name: "Document", exact: true });
    // The engine names the rule and the place (#1387), and the reason
    // crosses the gRPC hop and the API to the sentence the editor
    // shows.
    await expect(editor.getByRole("alert")).toHaveText(
      "Statement 1's Effect must be Allow or Deny.",
    );
    await expect(
      page.getByRole("textbox", { name: "Document JSON" }),
    ).toHaveAttribute("aria-invalid", "true");
    await expect(name).toHaveValue("Deny project renames");
    // The structured view marks the control the place names, and only
    // it.
    await page.getByRole("tab", { name: "Statements", exact: true }).click();
    const effect = page.getByRole("combobox", { name: "Statement 1 effect" });
    await expect(effect).toContainText("Maybe");
    await expect(effect).toHaveAttribute("aria-invalid", "true");
    await expect(
      page.getByRole("textbox", { name: "Statement 1 action 1" }),
    ).not.toHaveAttribute("aria-invalid");

    // A document naming the resource garam authorizes a project rename
    // against, so activated it denies the rename.
    await replaceJson(
      page,
      JSON.stringify({
        Version: "2026-07-25",
        Statement: [
          {
            Effect: "Deny",
            Action: ["project:update"],
            Resource: [`grn:${signedIn.orgSlug}:default:project:*`],
          },
        ],
      }),
    );
    await expect(editor.getByRole("alert")).toBeHidden();
    await create.click();
    await expect(heading(page, "Deny project renames")).toBeVisible();
    await expect(
      page.getByText("Draft: grants no access until it is activated.", {
        exact: true,
      }),
    ).toBeVisible();

    await page.getByRole("tab", { name: /^Attachments/ }).click();
    await clickUntil(
      page.getByRole("combobox", { name: "Team, person or agent" }),
      page.getByRole("option", { name: `${signedIn.groupName} · Team` }),
    );
    await page
      .getByRole("option", { name: `${signedIn.groupName} · Team` })
      .click();
    await page.getByRole("button", { name: "Attach", exact: true }).click();
    // Substring on purpose: a row's accessible name is all of its
    // cells.
    await expect(
      page.getByRole("row", { name: signedIn.groupName }),
    ).toContainText("Team");

    const dialog = page.getByRole("alertdialog", {
      name: "Activate Deny project renames?",
    });
    await clickUntil(
      page.getByRole("button", { name: "Activate", exact: true }),
      dialog,
    );
    await expect(dialog).toContainText(
      `Version 1 takes part in every check it reaches. Applies to ${signedIn.groupName} (Team).`,
    );
    await expect(dialog.getByRole("checkbox")).toHaveCount(0);

    // The control: while the policy is a draft, the seeded person may
    // still rename the seeded project, so the refusal below is the
    // policy taking effect.
    const rename = (name: string) =>
      page.request.patch(`/api/orgs/${signedIn.orgSlug}/projects/frontend`, {
        data: { name },
      });
    expect((await rename("Frontend draft")).status()).toBe(200);

    await dialog
      .getByRole("button", { name: "Activate policy", exact: true })
      .click();
    await expect(dialog).toBeHidden();
    // The state is said on the Document tab, above the editor.
    await page.getByRole("tab", { name: "Document", exact: true }).click();
    await expect(
      page.getByText(
        "Active: version 1 takes part in every check it reaches.",
        { exact: true },
      ),
    ).toBeVisible();
    expect((await rename("Frontend active")).status()).toBe(403);

    // The person reaches it through the team, not directly.
    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/permissions/attachments?subject=person:${signedIn.userSlug}`,
    );
    const through = page.getByRole("region", {
      name: "Through teams",
      exact: true,
    });
    await expect(
      through.getByRole("link", { name: "Deny project renames", exact: true }),
    ).toBeVisible();
    await expect(
      through.getByRole("link", { name: signedIn.groupName, exact: true }),
    ).toBeVisible();
    await expect(
      page
        .getByRole("region", { name: "Attached directly", exact: true })
        .getByText(`No policy is attached to ${signedIn.nickname}.`, {
          exact: true,
        }),
    ).toBeVisible();

    // The team's page opens its own attachments.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams/engineering`);
    await page
      .getByRole("link", { name: "Manage policies", exact: true })
      .click();
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: `${signedIn.groupName} · Team`,
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page
        .getByRole("region", { name: "Attached directly", exact: true })
        .getByRole("link", { name: "Deny project renames", exact: true }),
    ).toBeVisible();
  });

  test("asks to confirm everyone before activating a policy with no attachment", async ({
    page,
    signedIn,
  }) => {
    const policy = await createPolicy(
      page.request,
      signedIn,
      "Baseline read",
      JSON.stringify({
        Version: "2026-07-25",
        Statement: [
          {
            Effect: "Allow",
            Action: ["org:read"],
            Resource: [`grn:${signedIn.orgSlug}:*`],
          },
        ],
      }),
    );

    // The section's former address lands on Permissions.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/policies`);
    await expect(page).toHaveURL(
      `/console/orgs/${signedIn.orgSlug}/permissions`,
    );
    await page
      .getByRole("link", { name: "Baseline read", exact: true })
      .click();
    await expect(page).toHaveURL(policyUrl(signedIn, policy));

    const dialog = page.getByRole("alertdialog", {
      name: "Activate Baseline read?",
    });
    await clickUntil(
      page.getByRole("button", { name: "Activate", exact: true }),
      dialog,
    );
    await expect(dialog).toContainText(
      "Version 1 takes part in every check it reaches. No attachments: applies to everyone in this organization.",
    );
    const activate = dialog.getByRole("button", {
      name: "Activate for everyone",
      exact: true,
    });
    const confirm = dialog.getByRole("checkbox", {
      name: "Apply it to everyone in this organization",
      exact: true,
    });
    // Unconfirmed, the browser does not send the form.
    await activate.click();
    await expect(dialog).toBeVisible();
    await expect(
      page.getByText("Draft: grants no access until it is activated.", {
        exact: true,
      }),
    ).toBeAttached();
    await confirm.check();
    await activate.click();
    await expect(
      page.getByText(
        "Active: version 1 takes part in every check it reaches.",
        { exact: true },
      ),
    ).toBeVisible();

    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/permissions/attachments?subject=person:${signedIn.userSlug}`,
    );
    await expect(
      page
        .getByRole("region", { name: "Organization-wide", exact: true })
        .getByRole("link", { name: "Baseline read", exact: true }),
    ).toBeVisible();
  });

  test("keeps an active policy's last attachment and names the next step", async ({
    page,
    signedIn,
  }) => {
    const policy = await createPolicy(
      page.request,
      signedIn,
      "Deny project deletion",
      denyDocument(signedIn.orgSlug, ["project:delete"]),
    );
    await attachToTeamAndActivate(page.request, signedIn, policy);
    const second = await page.request.post(
      `/api/orgs/${signedIn.orgSlug}/policies/${policy}/attachments`,
      { data: { subjectGrn: signedIn.userGrn } },
    );
    expect(second.status()).toBe(201);

    await page.goto(policyUrl(signedIn, policy));
    await page.getByRole("tab", { name: /^Attachments/ }).click();
    const dialog = page.getByRole("alertdialog", {
      name: "Detach Deny project deletion?",
    });
    const confirm = dialog.getByRole("button", {
      name: "Detach policy",
      exact: true,
    });
    // The control: with the team still attached, detaching the person
    // goes through, so the refusal below is the last attachment's.
    await clickUntil(
      page.getByRole("button", {
        name: `Detach Deny project deletion from ${signedIn.nickname}`,
        exact: true,
      }),
      dialog,
    );
    await confirm.click();
    await expect(dialog).toBeHidden();
    // Substring on purpose: a row's accessible name is all of its
    // cells.
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toHaveCount(0);

    await clickUntil(
      page.getByRole("button", {
        name: `Detach Deny project deletion from ${signedIn.groupName}`,
        exact: true,
      }),
      dialog,
    );
    await confirm.click();

    const refusal = dialog.getByRole("alert");
    await expect(refusal).toContainText(
      "Not detached: this is its last attachment",
    );
    await expect(refusal).toContainText(
      "Attach it to another team, person or agent first, or archive the policy.",
    );

    await page.reload();
    await page.getByRole("tab", { name: /^Attachments/ }).click();
    // Substring on purpose: a row's accessible name is all of its
    // cells.
    await expect(
      page.getByRole("row", { name: signedIn.groupName }),
    ).toContainText("Team");
  });

  test("publishes an edit as the next version after its diff, and rolls back", async ({
    page,
    signedIn,
  }) => {
    const policy = await createPolicy(
      page.request,
      signedIn,
      "Deny project deletion",
      denyDocument(signedIn.orgSlug, ["project:delete"]),
    );
    await attachToTeamAndActivate(page.request, signedIn, policy);
    await page.goto(policyUrl(signedIn, policy));

    const publish = page.getByRole("button", {
      name: "Publish v2",
      exact: true,
    });
    // Nothing differs from the default yet, so nothing is offered.
    await expect(publish).toBeDisabled();
    await clickUntil(
      page.getByRole("button", { name: "Add action", exact: true }),
      page.getByRole("textbox", { name: "Statement 1 action 2" }),
    );
    await page
      .getByRole("textbox", { name: "Statement 1 action 2" })
      .fill("project:update");
    await expect(page.getByText("Edited, not published")).toBeVisible();

    await publish.click();
    const dialog = page.getByRole("alertdialog", {
      name: "Publish v2 of Deny project deletion?",
    });
    await expect(dialog).toContainText(
      `It becomes the default and applies at once. Applies to ${signedIn.groupName} (Team).`,
    );
    await expect(dialog.getByRole("figure")).toContainText(
      'Added: "project:update"',
    );
    await dialog
      .getByRole("button", { name: "Publish v2", exact: true })
      .click();
    await expect(
      page.getByText("Published v2.", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(
        "Active: version 2 takes part in every check it reaches.",
        { exact: true },
      ),
    ).toBeVisible();

    // v2 -> v1, from the versions list.
    await page.getByRole("tab", { name: /^Versions/ }).click();
    const rollBack = page.getByRole("alertdialog", {
      name: "Roll back to v1?",
    });
    await clickUntil(
      page
        .getByRole("row", { name: /^v1/ })
        .getByRole("button", { name: "Roll back to v1", exact: true }),
      rollBack,
    );
    await expect(rollBack).toContainText(
      `The policy evaluates v1 instead of v2. This applies at once. Applies to ${signedIn.groupName} (Team).`,
    );
    await rollBack
      .getByRole("button", { name: "Roll back to v1", exact: true })
      .click();
    await expect(
      page.getByRole("row", { name: /^v1/ }).getByText("Default · in use"),
    ).toBeVisible();
    await page.getByRole("tab", { name: "Document", exact: true }).click();
    await expect(
      page.getByText(
        "Active: version 1 takes part in every check it reaches.",
        { exact: true },
      ),
    ).toBeVisible();
  });

  test("shows a member who may not read policies a refusal, with nothing to create", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    const permissions = `/console/orgs/${signedIn.orgSlug}/permissions`;
    // The control: the owner is offered the create action on the page.
    await page.goto(permissions);
    await expect(
      page.getByRole("link", { name: "Create policy", exact: true }),
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

    await page.goto(permissions);
    await expect(
      page.getByText("You can't view this organization's policies", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Create policy", exact: true }),
    ).toBeHidden();
    // A refused list is not an empty one.
    await expect(
      page.getByText("This organization has no policies.", { exact: true }),
    ).toBeHidden();
  });
});

test.describe("permissions accessibility", () => {
  // The policy table stacks each policy's link over its identifier.
  // axe's target-size passes a target under 24px spaced off its
  // neighbours, as the table's gap spaces it, so the size is measured
  // as well as scanned.
  test("the policy table's identifiers are 24px targets", async ({
    page,
    signedIn,
  }) => {
    for (const name of ["Deny project deletion", "Deny project renames"]) {
      await createPolicy(
        page.request,
        signedIn,
        name,
        denyDocument(signedIn.orgSlug, ["project:delete"]),
      );
    }
    await page.goto(`/console/orgs/${signedIn.orgSlug}/permissions`);
    await expectHydrated(page);
    const copies = page
      .getByRole("table")
      .getByRole("button", { name: /^Copy the identifier of/ });
    await expect(copies).toHaveCount(2);

    for (const copy of await copies.all()) {
      const box = await copy.boundingBox();
      expect(box?.width).toBeGreaterThanOrEqual(24);
      expect(box?.height).toBeGreaterThanOrEqual(24);
    }
    // WCAG 2.2 AA, which carries target-size.
    expect(await axeViolations(page, ["table"])).toEqual([]);
  });

  for (const theme of ["light", "dark"] as const) {
    test(`every Permissions route passes axe in the ${theme} theme`, async ({
      page,
      signedIn,
    }) => {
      await useTheme(page, theme);
      const policy = await createPolicy(
        page.request,
        signedIn,
        "Deny project deletion",
        denyDocument(signedIn.orgSlug, ["project:delete"]),
      );
      await attachToTeamAndActivate(page.request, signedIn, policy);
      // A stored v2 beside the default v1, so a diff has both kinds of
      // line to scan.
      const saved = await page.request.post(
        `/api/orgs/${signedIn.orgSlug}/policies/${policy}/versions`,
        {
          data: {
            document: JSON.parse(
              denyDocument(signedIn.orgSlug, ["project:update"]),
            ),
          },
        },
      );
      expect(saved.status()).toBe(201);
      const base = `/console/orgs/${signedIn.orgSlug}/permissions`;

      for (const [path, title] of [
        [base, /^Permissions/],
        [`${base}/new`, /^New policy$/],
        [`${base}/attachments`, /^Permissions/],
        [
          `${base}/attachments?subject=person:${signedIn.userSlug}`,
          /^Permissions/,
        ],
        [`${base}/${policy}`, /^Deny project deletion$/],
      ] as const) {
        await page.goto(path);
        await expect(
          page.getByRole("heading", { level: 1, name: title }),
        ).toBeVisible();
        await expectHydrated(page);
        // The shell hydrates before this page's form, whose submit stays
        // disabled until its own part does; the scan reads it enabled.
        if (path.endsWith("/new")) {
          await expect(
            page.getByRole("button", { name: "Create draft" }),
          ).toBeEnabled();
        }
        expect(await axeViolations(page), path).toEqual([]);
      }

      // Each of the policy page's tabs, and the dialog publishing it.
      for (const tab of [/^Versions/, /^Attachments/]) {
        await page.getByRole("tab", { name: tab }).click();
        expect(await axeViolations(page), String(tab)).toEqual([]);
      }
      await page.getByRole("tab", { name: "Document" }).click();
      await page.getByRole("tab", { name: "JSON", exact: true }).click();
      await expect(
        page.getByRole("textbox", { name: "Document JSON" }),
      ).toBeVisible();
      expect(await axeViolations(page), "JSON view").toEqual([]);
      await page.getByRole("tab", { name: /^Versions/ }).click();
      await page
        .getByRole("row", { name: /^v1/ })
        .getByRole("button", { name: /^View/ })
        .click();
      await expect(page.getByRole("dialog")).toBeVisible();
      expect(await axeViolations(page, ['[role="dialog"]'])).toEqual([]);
      await page.keyboard.press("Escape");

      // v2 against the default: removed and added lines on their tints.
      await page
        .getByRole("row", { name: /^v2/ })
        .getByRole("button", { name: /^Compare/ })
        .click();
      const diff = page.getByRole("dialog").getByRole("figure");
      await expect(diff).toContainText('Added: "project:update"');
      await expect(diff).toContainText('Removed: "project:delete"');
      expect(await axeViolations(page, ['[role="dialog"]']), "diff").toEqual(
        [],
      );
    });
  }
});
