import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";
import { seedArchivedTemplate, seedCutoverDefinition, type Seed } from "./seed";

/** What the area's routes read, seeded into the test's organization. */
async function seedArea(signedIn: Seed) {
  const defined = await seedCutoverDefinition(signedIn, "Moving", "frozen");
  const kept = await seedArchivedTemplate(signedIn, "Researcher", {
    workspace: "papers",
  });
  return {
    agents: `/console/orgs/${signedIn.orgSlug}/agents`,
    defined,
    kept: kept.identifier,
  };
}

// The Agents and Templates routes at 390px: a table scrolls inside its
// own container, never the page. One test reads one route.
const routes: {
  name: string;
  href: (area: Awaited<ReturnType<typeof seedArea>>) => string;
  heading: string;
}[] = [
  { name: "the agent list", href: (a) => a.agents, heading: "Agents" },
  {
    name: "an agent",
    href: (a) => `${a.agents}/${a.defined}`,
    heading: "Moving",
  },
  {
    name: "the new agent wizard",
    href: (a) => `${a.agents}/new`,
    heading: "New agent",
  },
  {
    name: "the template list",
    href: (a) => `${a.agents}/templates`,
    heading: "Templates",
  },
  {
    name: "an archived template",
    href: (a) => `${a.agents}/templates/${a.kept}`,
    heading: "Researcher",
  },
];

test.describe("agents and templates on a phone", () => {
  for (const route of routes) {
    test(`${route.name} scrolls no wider than the screen and passes axe`, async ({
      page,
      signedIn,
    }) => {
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

      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow).toBeLessThanOrEqual(0);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
