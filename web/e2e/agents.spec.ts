import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import { seedCutoverDefinition, seedDefinition, type Seed } from "./seed";

test.describe("no garam form", () => {
  test("the new agent page offers no garam form, and garam accepts no definition", async ({
    page,
    signedIn,
  }) => {
    // The new agent page offers the control service alone: no form to
    // define an agent in garam (ADR-0090). Its control service step is
    // the control that the page rendered.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/new`);
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: "Control service",
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.getByLabel("Operator", { exact: true })).toHaveCount(0);
    await expect(page.getByRole("textbox")).toHaveCount(0);

    // The define route is closed at garam itself.
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

    await page.getByRole("tab", { name: "Settings", exact: true }).click();
    const rename = page.getByLabel("Name", { exact: true });
    await expect(rename).toBeEnabled();
    await rename.fill("Ranger");
    await page.getByRole("button", { name: "Save name" }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Ranger.");
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    const row = page.getByRole("row").filter({ hasText: identifier });
    await expect(
      row.getByRole("link", { name: "Ranger", exact: true }),
    ).toBeVisible();
    await expect(row.getByText("Not placed")).toBeVisible();
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
    await expectHydrated(page);
    await page
      .getByRole("combobox", { name: "Person or agent", exact: true })
      .click();
    await page
      .getByRole("option", {
        name: `Ranger ${identifier} · awaiting operator`,
        exact: true,
      })
      .click();
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
  test("lists a failing reported agent first among the agents, in one state", async ({
    page,
    signedIn,
  }) => {
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);

    // The seeded agent reported a failed provisioning and a lost
    // connection, so it leads and reads as failing, with both reports
    // named under that state. The list holds agents alone (ADR-0116).
    await expect(
      page.getByRole("row", { name: signedIn.nickname }),
    ).toHaveCount(0);
    const first = page.getByRole("row").nth(1);
    await expect(
      first.getByText(signedIn.agentIdentifier, { exact: true }),
    ).toBeVisible();
    await expect(first.getByText("Failing", { exact: true })).toBeVisible();
    await expect(
      first.getByText("Provisioning failed; disconnected", { exact: true }),
    ).toBeVisible();
  });
});

test.describe("cutover", () => {
  test("no cut-over marker is shown where definitions are listed or read (#1592)", async ({
    page,
    signedIn,
  }) => {
    const switched = await seedCutoverDefinition(signedIn, "Moved", "switched");
    const frozen = await seedCutoverDefinition(signedIn, "Moving", "frozen");
    const agents = `/console/orgs/${signedIn.orgSlug}/agents`;
    const operator = `/console/orgs/${signedIn.orgSlug}/operators/${signedIn.operatorIdentifier}`;

    for (const [href, names] of [
      [agents, ["Moved", "Moving"]],
      [operator, ["Moved", "Moving"]],
      [`${agents}/${switched}`, ["Moved"]],
      [`${agents}/${frozen}`, ["Moving"]],
    ] as const) {
      await page.goto(href);
      // The control: each definition rendered, by its name.
      for (const name of names) {
        await expect(
          page.getByText(name, { exact: true }).first(),
        ).toBeVisible();
      }
      await expect(page.getByText("Managed by the Operator")).toHaveCount(0);
      await expect(page.getByText("Cut-over in progress")).toHaveCount(0);
      await expect(page.getByText(/archival/)).toHaveCount(0);
    }
  });
});

test.describe("agents accessibility", () => {
  /** What the area's routes read, seeded into the test's organization. */
  async function seedArea(signedIn: Seed) {
    const defined = await seedDefinition(signedIn, "Moving");
    return { agents: `/console/orgs/${signedIn.orgSlug}/agents`, defined };
  }

  // Each route, with the heading that says it rendered, and each tab a
  // detail page hides until it is opened: one test reads one of them.
  const routes: {
    name: string;
    href: (area: Awaited<ReturnType<typeof seedArea>>) => string;
    heading: string;
    tab?: string;
  }[] = [
    {
      name: "the agent list",
      href: (a) => a.agents,
      heading: "Agents",
    },
    {
      name: "an agent",
      href: (a) => `${a.agents}/${a.defined}`,
      heading: "Moving",
    },
    {
      name: "an agent's Execution tab",
      href: (a) => `${a.agents}/${a.defined}`,
      heading: "Moving",
      tab: "Execution",
    },
    {
      name: "an agent's Settings tab",
      href: (a) => `${a.agents}/${a.defined}`,
      heading: "Moving",
      tab: "Settings",
    },
    {
      name: "the new agent wizard",
      href: (a) => `${a.agents}/new`,
      heading: "New agent",
    },
  ];

  for (const theme of ["light", "dark"] as const) {
    for (const route of routes) {
      test(`${route.name} passes axe in the ${theme} theme`, async ({
        page,
        signedIn,
      }) => {
        await useTheme(page, theme);
        const href = route.href(await seedArea(signedIn));
        await page.goto(href);
        // A list's heading carries its count after the title.
        await expect(
          page.getByRole("heading", {
            level: 1,
            name: new RegExp(`^${route.heading}( ?\\d+)?$`),
          }),
        ).toBeVisible();
        await expectHydrated(page);
        if (route.tab !== undefined) {
          const tab = page.getByRole("tab", { name: route.tab, exact: true });
          await tab.click();
          await expect(tab).toHaveAttribute("aria-selected", "true");
        }
        expect(await axeViolations(page)).toEqual([]);
      });
    }
  }
});
