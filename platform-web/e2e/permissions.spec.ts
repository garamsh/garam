import type { APIRequestContext, Locator, Page } from "@playwright/test";
import { z } from "zod";

import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, holdSession, test } from "./fixtures";
import { seedMember, type Seed } from "./seed";
import { formatGrn } from "../src/lib/grn";

/** Opens a tab of the page and waits until it is the one shown. */
async function openTab(page: Page, name: string | RegExp): Promise<void> {
  const tab = page.getByRole("tab", { name, exact: typeof name === "string" });
  await tab.click();
  await expect(tab).toHaveAttribute("aria-selected", "true");
}

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

/** One Cedar `forbid` over an action, which a guardrail holds. */
function forbid(action: string): string {
  return `forbid (principal, action == Iam::Action::"${action}", resource);`;
}

const CreatedPolicy = z.object({ id: z.string() });

/**
 * Creates a guardrail through the API the console calls, for a test
 * whose subject is what the console does with it. Answers its
 * identifier.
 */
async function createGuardrail(
  request: APIRequestContext,
  seed: Seed,
  name: string,
  action = "agent.activate",
): Promise<string> {
  const created = await request.post(`/api/orgs/${seed.orgSlug}/iam-policies`, {
    data: { name, body: { kind: "guardrail", text: forbid(action) } },
  });
  expect(created.status()).toBe(201);
  return CreatedPolicy.parse(await created.json()).id;
}

/** Creates a role link of the seeded team over the organization. */
async function createRoleLink(
  request: APIRequestContext,
  seed: Seed,
  name: string,
): Promise<string> {
  const created = await request.post(`/api/orgs/${seed.orgSlug}/iam-policies`, {
    data: {
      name,
      body: {
        kind: "role-link",
        roleLink: {
          role: "admin",
          scope: "organization",
          principal: formatGrn(seed.orgSlug, "iam", "group", "engineering"),
          resource: formatGrn("root", "iam", "org", seed.orgSlug),
        },
      },
    },
  });
  expect(created.status()).toBe(201);
  return CreatedPolicy.parse(await created.json()).id;
}

/** Stores a second version of a guardrail, which is not the default. */
async function storeVersion(
  request: APIRequestContext,
  seed: Seed,
  policy: string,
  action: string,
): Promise<void> {
  const saved = await request.post(
    `/api/orgs/${seed.orgSlug}/iam-policies/${policy}/versions`,
    { data: { body: { kind: "guardrail", text: forbid(action) } } },
  );
  expect(saved.status()).toBe(201);
}

test.describe("permissions", () => {
  test("lists the policies by kind and state, each opening its page", async ({
    page,
    signedIn,
  }) => {
    const guardrail = await createGuardrail(
      page.request,
      signedIn,
      "Deny project deletion",
    );
    await createRoleLink(page.request, signedIn, "Engineering admins");

    await page.goto(`/console/orgs/${signedIn.orgSlug}/permissions`);

    const row = page.getByRole("row", { name: /Deny project deletion/ });
    await expect(row).toContainText("Guardrail");
    await expect(row).toContainText("Draft");
    await expect(row).toContainText("v1");
    await expect(
      page.getByRole("row", { name: /Engineering admins/ }),
    ).toContainText("Role link");
    // Creating one is not offered here: the page says where it is done.
    await expect(
      page.getByText("A policy is created through the API or the CLI."),
    ).toBeVisible();
    await expect(page.getByRole("link", { name: "Create policy" })).toHaveCount(
      0,
    );

    await page.getByRole("link", { name: "Deny project deletion" }).click();

    await expect(page).toHaveURL(
      `/console/orgs/${signedIn.orgSlug}/permissions/${guardrail}`,
    );
    await expect(
      page.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeVisible();
    await expect(page.getByLabel("Cedar text")).toContainText(
      forbid("agent.activate"),
    );
  });

  test("shows a role link's role, scope, principal and resource", async ({
    page,
    signedIn,
  }) => {
    const roleLink = await createRoleLink(
      page.request,
      signedIn,
      "Engineering admins",
    );

    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/permissions/${roleLink}`,
    );

    await expect(page.getByText("Role link", { exact: true })).toBeVisible();
    await expect(page.getByText("admin", { exact: true })).toBeVisible();
    await expect(
      page.getByLabel("Definition").getByText("Organization", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(
        formatGrn(signedIn.orgSlug, "iam", "group", "engineering"),
        {
          exact: true,
        },
      ),
    ).toBeVisible();
  });

  test("activates a draft, then makes another version the default", async ({
    page,
    signedIn,
  }) => {
    const policy = await createGuardrail(
      page.request,
      signedIn,
      "Deny project deletion",
    );
    await storeVersion(page.request, signedIn, policy, "agent.configure");
    await page.goto(`/console/orgs/${signedIn.orgSlug}/permissions/${policy}`);
    await expectHydrated(page);
    await expect(page.getByText("Draft", { exact: true })).toBeVisible();

    await clickUntil(
      page.getByRole("button", { name: "Activate", exact: true }),
      page.getByRole("alertdialog"),
    );
    await page
      .getByRole("alertdialog")
      .getByRole("button", { name: "Activate policy" })
      .click();

    await expect(page.getByText("Activated v1")).toBeVisible();
    await expect(page.getByText("Active", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Activate", exact: true }),
    ).toHaveCount(0);

    await openTab(page, /^Versions/);
    await page
      .getByRole("row", { name: /^v2/ })
      .getByRole("button", { name: "Make v2 default" })
      .click();
    await page
      .getByRole("alertdialog")
      .getByRole("button", { name: "Make v2 default" })
      .click();

    await expect(page.getByText("v2 is now the default.")).toBeVisible();
    await expect(
      page.getByRole("row", { name: /^v2/ }).getByText("In use"),
    ).toBeVisible();
  });

  test("deletes a policy once its name is typed, and opens the list without it", async ({
    page,
    signedIn,
  }) => {
    const policy = await createGuardrail(
      page.request,
      signedIn,
      "Deny project deletion",
    );
    await page.goto(`/console/orgs/${signedIn.orgSlug}/permissions/${policy}`);
    await expectHydrated(page);

    // The danger zone closes the last tab.
    await openTab(page, /^Versions/);
    await clickUntil(
      page.getByRole("button", { name: "Delete policy", exact: true }),
      page.getByRole("alertdialog"),
    );
    const dialog = page.getByRole("alertdialog");
    const confirm = dialog.getByRole("button", { name: "Delete policy" });
    await expect(confirm).toBeDisabled();
    await dialog.getByRole("textbox").fill("Deny project deletion");
    await confirm.click();

    await expect(page).toHaveURL(
      `/console/orgs/${signedIn.orgSlug}/permissions`,
    );
    await expect(
      page.getByRole("link", { name: "Deny project deletion" }),
    ).toHaveCount(0);
    // The control: the list is read, and holds the owner's role link
    // an organization is created with.
    await expect(
      page.getByRole("link", { name: "role link owner" }),
    ).toBeVisible();
  });

  test("shows a member who may not read policies a refusal", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    await createGuardrail(page.request, signedIn, "Deny project deletion");
    const permissions = `/console/orgs/${signedIn.orgSlug}/permissions`;
    // The control: the owner reads the list.
    await page.goto(permissions);
    await expect(
      page.getByRole("link", { name: "Deny project deletion" }),
    ).toBeVisible();

    const member = await seedMember(signedIn);
    await context.clearCookies();
    await holdSession(context, baseURL, member.sessionToken);

    await page.goto(permissions);
    await expect(
      page.getByText("You can't view this organization's policies", {
        exact: true,
      }),
    ).toBeVisible();
    // A refused list is not an empty one.
    await expect(page.getByText("No policies", { exact: true })).toBeHidden();
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
      await createGuardrail(page.request, signedIn, name);
    }
    await page.goto(`/console/orgs/${signedIn.orgSlug}/permissions`);
    await expectHydrated(page);
    const copies = page
      .getByRole("table")
      .getByRole("button", { name: /^Copy the identifier of/ });
    // The two made here and the owner's role link, which an
    // organization is created with.
    await expect(copies).toHaveCount(3);

    for (const copy of await copies.all()) {
      const box = await copy.boundingBox();
      expect(box?.width).toBeGreaterThanOrEqual(24);
      expect(box?.height).toBeGreaterThanOrEqual(24);
    }
    // WCAG 2.2 AA, which carries target-size.
    expect(await axeViolations(page, ["table"])).toEqual([]);
  });

  /**
   * What the area's routes read: a guardrail with a stored v2 beside
   * the default v1, so a diff has both kinds of line to scan.
   */
  async function seedArea(request: APIRequestContext, signedIn: Seed) {
    const policy = await createGuardrail(
      request,
      signedIn,
      "Deny project deletion",
    );
    await storeVersion(request, signedIn, policy, "agent.configure");
    return { base: `/console/orgs/${signedIn.orgSlug}/permissions`, policy };
  }

  // One test reads one of these: a route, a tab of the policy page, or
  // a state a step on it reaches. `reach` waits for what the scan
  // reads, and `include` limits the scan to a dialog.
  const views: {
    name: string;
    path: (area: Awaited<ReturnType<typeof seedArea>>) => string;
    title: RegExp;
    reach?: (page: Page) => Promise<void>;
    include?: string[];
  }[] = [
    { name: "the policy list", path: (a) => a.base, title: /^Permissions/ },
    {
      name: "a policy",
      path: (a) => `${a.base}/${a.policy}`,
      title: /^Deny project deletion$/,
    },
    {
      name: "a policy's Versions tab",
      path: (a) => `${a.base}/${a.policy}`,
      title: /^Deny project deletion$/,
      reach: (page) => openTab(page, /^Versions/),
    },
    {
      name: "a stored version's dialog",
      path: (a) => `${a.base}/${a.policy}`,
      title: /^Deny project deletion$/,
      reach: async (page) => {
        await openTab(page, /^Versions/);
        await page
          .getByRole("row", { name: /^v1/ })
          .getByRole("button", { name: /^View/ })
          .click();
        await expect(page.getByRole("dialog")).toBeVisible();
      },
      include: ['[role="dialog"]'],
    },
    {
      name: "a diff against the default",
      path: (a) => `${a.base}/${a.policy}`,
      title: /^Deny project deletion$/,
      // v2 against the default: removed and added lines on their tints.
      reach: async (page) => {
        await openTab(page, /^Versions/);
        await page
          .getByRole("row", { name: /^v2/ })
          .getByRole("button", { name: /^Compare/ })
          .click();
        const diff = page.getByRole("dialog").getByRole("figure");
        await expect(diff).toContainText("agent.configure");
        await expect(diff).toContainText("agent.activate");
      },
      include: ['[role="dialog"]'],
    },
  ];

  for (const theme of ["light", "dark"] as const) {
    for (const view of views) {
      test(`${view.name} passes axe in the ${theme} theme`, async ({
        page,
        signedIn,
      }) => {
        await useTheme(page, theme);
        const path = view.path(await seedArea(page.request, signedIn));
        await page.goto(path);
        await expect(
          page.getByRole("heading", { level: 1, name: view.title }),
        ).toBeVisible();
        await expectHydrated(page);
        await view.reach?.(page);
        expect(await axeViolations(page, view.include)).toEqual([]);
      });
    }
  }
});
