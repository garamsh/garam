import { expect, test } from "./fixtures";
import {
  seedArchivedTemplate,
  seedCutoverDefinition,
  seedDefinition,
} from "./seed";

test.describe("archived agent templates", () => {
  test("a kept template is a record only, and the new agent page offers no garam form", async ({
    page,
    signedIn,
  }) => {
    const agents = `/console/orgs/${signedIn.orgSlug}/agents`;
    const kept = await seedArchivedTemplate(signedIn, "Researcher", {
      workspace: "papers",
    });

    // Listed under its own heading, with no way to make another.
    await page.goto(`${agents}/templates`);
    await expect(
      page.getByRole("heading", { level: 2, name: "Archived garam templates" }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "New template", exact: true }),
    ).toHaveCount(0);

    // Its page shows what it said and offers nothing to do with it.
    await page.getByRole("link", { name: "Researcher", exact: true }).click();
    await expect(
      page.getByRole("heading", { level: 1, name: "Researcher", exact: true }),
    ).toBeVisible();
    await expect(page.getByText("papers", { exact: true })).toBeVisible();
    await expect(
      page.getByText(kept.identifier, { exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("textbox")).toHaveCount(0);
    await expect(
      page.getByRole("link", { name: "Create agent from template" }),
    ).toHaveCount(0);

    // The new agent page offers the control service alone: no garam
    // template and no form to define an agent in garam (ADR-0090). Its
    // control service section is the control that the page rendered.
    await page.goto(`${agents}/new?template=${kept.identifier}`);
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: "From the control service",
      }),
    ).toBeVisible();
    await expect(page.getByText("Researcher")).toHaveCount(0);
    await expect(page.getByLabel("Operator", { exact: true })).toHaveCount(0);
    await expect(page.getByRole("textbox")).toHaveCount(0);

    // The template's write routes and the define route are closed at
    // garam itself.
    const replaced = await page.request.put(
      `/api/orgs/${signedIn.orgSlug}/agent-templates/${kept.identifier}`,
      { data: { name: "Analyst", values: {} } },
    );
    expect(replaced.status()).toBe(405);
    const defined = await page.request.post(
      `/api/orgs/${signedIn.orgSlug}/agent-definitions`,
      {
        data: {
          name: "Scout",
          operatorGrn: `grn:${signedIn.orgSlug}:default:operator:${signedIn.operatorIdentifier}`,
          values: { model: "haiku" },
        },
      },
    );
    expect(defined.status()).toBe(405);
  });
});

test.describe("named agents", () => {
  test("renaming moves the name and keeps the identifier", async ({
    page,
    signedIn,
  }) => {
    const identifier = await seedDefinition(signedIn, "Scout");
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/${identifier}`);

    const rename = page.getByLabel("Name", { exact: true });
    await expect(rename).toBeEnabled();
    await rename.fill("Ranger");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Ranger.");
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    const card = page.getByRole("listitem").filter({ hasText: identifier });
    await expect(
      card.getByRole("link", { name: "Ranger", exact: true }),
    ).toBeVisible();
    await expect(card.getByText("Awaiting operator")).toBeVisible();
  });

  test("a team and a policy take a defined agent by name before any claim", async ({
    page,
    signedIn,
  }) => {
    const identifier = await seedDefinition(signedIn, "Ranger");

    // A team takes the agent by name before any operator claims it: the
    // membership is its GRN, minted with the definition.
    const team = `/console/orgs/${signedIn.orgSlug}/teams/engineering`;
    await page.goto(team);
    const option = page.getByRole("option", {
      name: `Ranger ${identifier} · awaiting operator`,
      exact: true,
    });
    // A click taken before React hydrates the choice is dropped, so it
    // is repeated until the options are there.
    await expect(async () => {
      await page
        .getByRole("combobox", { name: "Person or agent", exact: true })
        .click();
      await expect(option).toBeVisible({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });
    await option.click();
    await page
      .getByRole("button", { name: "Add to team", exact: true })
      .click();
    const row = page.getByRole("row", { name: /Ranger/ });
    await expect(row).toContainText("Agent");
    await page.reload();
    await expect(page.getByRole("row", { name: /Ranger/ })).toContainText(
      "Agent",
    );

    // A policy can be given to it by the same name: its policies are
    // shown under that name.
    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/permissions/attachments?subject=${encodeURIComponent(`agent:${identifier}`)}`,
    );
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: `Ranger (${identifier}) · awaiting operator · Agent`,
        exact: true,
      }),
    ).toBeVisible();
  });
});

test.describe("agents", () => {
  test("shows a reported agent and the states its two parties reported", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);

    await expect(
      page.getByText(signedIn.agentIdentifier, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.agentProvisioningState, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.agentRuntimeState, { exact: true }),
    ).toBeVisible();
  });
});

test.describe("cutover", () => {
  test("a definition cutting over or cut over is marked archival, and only its name changes", async ({
    page,
    signedIn,
  }) => {
    const switched = await seedCutoverDefinition(signedIn, "Moved", "switched");
    const frozen = await seedCutoverDefinition(signedIn, "Moving", "frozen");
    // The control: an attempt rolled back is none open, so its
    // definition reads as a legacy one does.
    const legacy = await seedCutoverDefinition(
      signedIn,
      "Stayed",
      "rolled_back",
    );
    const agents = `/console/orgs/${signedIn.orgSlug}/agents`;
    const operator = `/console/orgs/${signedIn.orgSlug}/operators/${signedIn.operatorIdentifier}`;

    const names = new Map([
      [switched, "Moved"],
      [frozen, "Moving"],
      [legacy, "Stayed"],
    ]);
    // Where definitions are listed: the roster, by identifier, and the
    // operator's page, which names an agent and prints no identifier.
    for (const [href, row] of [
      [
        agents,
        (id: string) => page.getByRole("listitem").filter({ hasText: id }),
      ],
      [
        operator,
        (id: string) =>
          page
            .getByRole("region", { name: "Agents it runs" })
            .getByRole("listitem")
            .filter({
              has: page.getByRole("link", { name: names.get(id), exact: true }),
            }),
      ],
    ] as const) {
      await page.goto(href);
      await expect(
        row(switched).getByText("Managed by the Operator", { exact: true }),
      ).toBeVisible();
      await expect(
        row(frozen).getByText("Cut-over in progress", { exact: true }),
      ).toBeVisible();
      await expect(row(legacy)).toBeVisible();
      await expect(
        row(legacy).getByText("Managed by the Operator"),
      ).toHaveCount(0);
      await expect(row(legacy).getByText("Cut-over in progress")).toHaveCount(
        0,
      );
    }

    const archival =
      "These settings are archival and read-only in garam; only the name still changes here.";
    await page.goto(`${agents}/${frozen}`);
    await expect(
      page.getByText(`Cut-over in progress. ${archival}`, { exact: true }),
    ).toBeVisible();

    await page.goto(`${agents}/${legacy}`);
    await expect(
      page.getByText("Saved values; application is not reported.", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.getByText(archival)).toHaveCount(0);

    // A switched definition's values are archival, and its name is the
    // one thing garam still changes (ADR-0086).
    await page.goto(`${agents}/${switched}`);
    await expect(
      page.getByText(`Managed by the Operator. ${archival}`, { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("haiku", { exact: true })).toBeVisible();
    const rename = page.getByLabel("Name", { exact: true });
    await expect(rename).toBeEnabled();
    await rename.fill("Moved on");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Moved on.");
  });
});
