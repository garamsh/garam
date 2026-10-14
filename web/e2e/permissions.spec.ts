import type { APIRequestContext, Locator, Page } from "@playwright/test";
import { z } from "zod";

import { expect, test } from "./fixtures";
import type { Seed } from "./seed";

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

/** A document denying one action across the seeded organization. */
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

    // A document the engine refuses leaves what was typed in place.
    const name = page.getByLabel("Policy name", { exact: true });
    const document = page.getByLabel("Document", { exact: true });
    // The editor takes input once it can hold it, and a fill waits for
    // that. The starting document is kept to be sent unchanged below.
    await expect(document).toBeEnabled();
    const seeded = await document.inputValue();
    const refused = JSON.stringify({
      Version: "2026-07-25",
      Statement: [
        { Effect: "Maybe", Action: ["project:delete"], Resource: ["*"] },
      ],
    });
    await name.fill("Deny project renames");
    await document.fill(refused);
    await page
      .getByRole("button", { name: "Create draft", exact: true })
      .click();
    await expect(
      page.getByText(
        "This policy document can't be saved. It needs a Version and at least one statement, and every statement needs an Effect of Allow or Deny, an Action and a Resource. Keys other than Version, Statement, Sid, Effect, Action, Resource and Condition aren't allowed.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(name).toHaveValue("Deny project renames");
    await expect(document).toHaveValue(refused);

    await document.fill(seeded);
    await page
      .getByRole("button", { name: "Create draft", exact: true })
      .click();
    await expect(heading(page, "Deny project renames")).toBeVisible();
    await expect(
      page.getByText("Draft: grants no access until it is activated.", {
        exact: true,
      }),
    ).toBeVisible();

    await page
      .getByLabel("Team, person or agent", { exact: true })
      .selectOption({ label: `${signedIn.groupName} · Team` });
    await page.getByRole("button", { name: "Attach", exact: true }).click();
    // Substring on purpose: a row's accessible name is all of its
    // cells.
    await expect(
      page.getByRole("row", { name: signedIn.groupName }),
    ).toContainText("Team");

    const activate = page.getByRole("button", {
      name: "Activate",
      exact: true,
    });
    await clickUntil(
      page.getByRole("button", { name: "Activate…", exact: true }),
      activate,
    );
    await expect(
      page.getByText(
        `Activate Deny project renames with version 1? Applies to ${signedIn.groupName} (Team).`,
        { exact: true },
      ),
    ).toBeVisible();
    await expect(page.getByRole("checkbox")).toHaveCount(0);

    // The control: while the policy is a draft, the seeded person may
    // still rename the seeded project, so the refusal below is the
    // policy taking effect.
    const rename = (name: string) =>
      page.request.patch(`/api/orgs/${signedIn.orgSlug}/projects/frontend`, {
        data: { name },
      });
    expect((await rename("Frontend draft")).status()).toBe(200);

    await activate.click();
    await expect(
      page.getByText(
        "Active: version 1 takes part in every check it reaches.",
        { exact: true },
      ),
    ).toBeVisible();
    // The starting document names the resource garam authorizes a
    // project rename against, so activated it denies the rename.
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

    const activate = page.getByRole("button", {
      name: "Activate for everyone",
      exact: true,
    });
    await clickUntil(
      page.getByRole("button", { name: "Activate…", exact: true }),
      activate,
    );
    await expect(
      page.getByText(
        "Activate Baseline read with version 1? No attachments: applies to everyone in this organization.",
        { exact: true },
      ),
    ).toBeVisible();
    const confirm = page.getByRole("checkbox", {
      name: "Apply it to everyone in this organization",
      exact: true,
    });
    // Unconfirmed, the browser does not send the form.
    await activate.click();
    await expect(
      page.getByText("Draft: grants no access until it is activated.", {
        exact: true,
      }),
    ).toBeVisible();
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
    const confirm = page.getByRole("button", {
      name: "Confirm detach",
      exact: true,
    });
    // The control: with the team still attached, detaching the person
    // goes through, so the refusal below is the last attachment's.
    await clickUntil(
      page.getByRole("button", {
        name: `Detach Deny project deletion from ${signedIn.nickname}`,
        exact: true,
      }),
      confirm,
    );
    await confirm.click();
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
      confirm,
    );
    await confirm.click();

    await expect(
      page.getByText(
        "Not detached: this is the last attachment of an active policy. Without it, the policy would apply to everyone in this organization.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(
      page.getByText(
        "Attach it to another team, person or agent first, or archive the policy.",
        { exact: true },
      ),
    ).toBeVisible();

    await page.reload();
    // Substring on purpose: a row's accessible name is all of its
    // cells.
    await expect(
      page.getByRole("row", { name: signedIn.groupName }),
    ).toContainText("Team");
    await expect(
      page.getByText(`Applies to ${signedIn.groupName} (Team).`, {
        exact: true,
      }),
    ).toBeVisible();
  });

  test("saves a version, makes it the default and rolls back", async ({
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

    const editor = page.getByLabel("New version", { exact: true });
    await expect(editor).toBeEnabled();
    await editor.fill(
      denyDocument(signedIn.orgSlug, ["project:delete", "project:update"]),
    );
    await page
      .getByRole("button", { name: "Save version", exact: true })
      .click();
    await expect(
      page.getByText(
        "Saved as version 2. Version 1 is still the default; make version 2 the default to use it.",
        { exact: true },
      ),
    ).toBeVisible();

    const second = page.getByRole("listitem", {
      name: "Version 2",
      exact: true,
    });
    const first = page.getByRole("listitem", {
      name: "Version 1",
      exact: true,
    });
    await expect(first.getByText("Default", { exact: true })).toBeVisible();

    // v1 -> v2.
    const makeTwo = page.getByRole("button", {
      name: "Make version 2 default",
      exact: true,
    });
    await clickUntil(
      second.getByRole("button", { name: "Make default", exact: true }),
      makeTwo,
    );
    await expect(
      page.getByText(
        `Evaluate version 2 instead of version 1? This applies at once. Applies to ${signedIn.groupName} (Team).`,
        { exact: true },
      ),
    ).toBeVisible();
    await makeTwo.click();
    await expect(second.getByText("Default", { exact: true })).toBeVisible();
    await expect(
      page.getByText(
        "Active: version 2 takes part in every check it reaches.",
        { exact: true },
      ),
    ).toBeVisible();

    // v2 -> v1.
    const rollBack = page.getByRole("button", {
      name: "Roll back to version 1",
      exact: true,
    });
    await clickUntil(
      first.getByRole("button", { name: "Roll back to v1", exact: true }),
      rollBack,
    );
    await rollBack.click();
    await expect(first.getByText("Default", { exact: true })).toBeVisible();
    await expect(
      page.getByText(
        "Active: version 1 takes part in every check it reaches.",
        { exact: true },
      ),
    ).toBeVisible();
  });
});
