import { axeViolations, expectHydrated, useTheme } from "./axe";
import { expect, test } from "./fixtures";
import type { Seed } from "./seed";

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

    // garam serves no definition route at all (ADR-0118).
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
    expect(defined.status()).toBe(404);
  });
});

test.describe("an agent's name", () => {
  test("the owner names an agent, which the list then shows, and clears it again (ADR-0119)", async ({
    page,
    signedIn,
  }) => {
    const identifier = signedIn.agentIdentifier;
    const agentPage = `/console/orgs/${signedIn.orgSlug}/agents/${identifier}`;
    await page.goto(agentPage);
    await expectHydrated(page);
    const name = page.getByRole("textbox", { name: "Name", exact: true });
    await name.fill("Sherlock");
    await page.getByRole("button", { name: "Save name", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("Renamed to Sherlock.");

    await page.reload();
    await expect(
      page.getByRole("heading", { level: 1, name: "Sherlock", exact: true }),
    ).toBeVisible();
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents`);
    await expect(
      page.getByRole("link", { name: "Sherlock", exact: true }),
    ).toBeVisible();

    // The control: cleared, the agent is titled by its identifier
    // again.
    await page.goto(agentPage);
    await expectHydrated(page);
    await name.fill("");
    await page.getByRole("button", { name: "Save name", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("Name cleared.");
    await page.reload();
    await expect(
      page.getByRole("heading", { level: 1, name: identifier, exact: true }),
    ).toBeVisible();
  });
});

test.describe("an agent as a member", () => {
  test("a team and a policy take an agent by its identifier", async ({
    page,
    signedIn,
  }) => {
    const identifier = signedIn.agentIdentifier;

    // An agent with no name is offered by its identifier, which the
    // choice also shows beside it (ADR-0119).
    const team = `/console/orgs/${signedIn.orgSlug}/teams/engineering`;
    await page.goto(team);
    await expectHydrated(page);
    await page
      .getByRole("combobox", { name: "Person or agent", exact: true })
      .click();
    await page
      .getByRole("option", {
        name: `${identifier} ${identifier}`,
        exact: true,
      })
      .click();
    await page
      .getByRole("button", { name: "Add to team", exact: true })
      .click();
    const row = page.getByRole("row", { name: new RegExp(identifier) });
    await expect(row).toContainText("Agent");
    await page.reload();
    await expect(
      page.getByRole("row", { name: new RegExp(identifier) }),
    ).toContainText("Agent");

    // A policy can be given to it the same way.
    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/permissions/attachments?subject=${encodeURIComponent(`agent:${identifier}`)}`,
    );
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: `${identifier} · Agent`,
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
    // With no name, the agent's link is its identifier (ADR-0119).
    await expect(
      first.getByRole("link", { name: signedIn.agentIdentifier, exact: true }),
    ).toBeVisible();
    await expect(first.getByText("Failing", { exact: true })).toBeVisible();
    await expect(
      first.getByText("Provisioning failed; disconnected", { exact: true }),
    ).toBeVisible();
  });
});

test.describe("agents accessibility", () => {
  /** What the area's routes read, seeded into the test's organization. */
  async function seedArea(signedIn: Seed) {
    return {
      agents: `/console/orgs/${signedIn.orgSlug}/agents`,
      agent: signedIn.agentIdentifier,
    };
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
      href: (a) => `${a.agents}/${a.agent}`,
      // An agent with no name is titled by its identifier (ADR-0119),
      // which the seed draws, so the heading matches its shape.
      heading: "[0-9a-f]{16}",
    },
    {
      name: "an agent's Execution tab",
      href: (a) => `${a.agents}/${a.agent}`,
      // An agent with no name is titled by its identifier (ADR-0119),
      // which the seed draws, so the heading matches its shape.
      heading: "[0-9a-f]{16}",
      tab: "Execution",
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
