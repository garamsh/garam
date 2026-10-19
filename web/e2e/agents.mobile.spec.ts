import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";
import type { Seed } from "./seed";

/** What the area's routes read, seeded into the test's organization. */
async function seedArea(signedIn: Seed) {
  return {
    agents: `/console/orgs/${signedIn.orgSlug}/agents`,
    agent: signedIn.agentIdentifier,
  };
}

// The Agents routes at 390px: a table scrolls inside its
// own container, never the page. One test reads one route.
const routes: {
  name: string;
  href: (area: Awaited<ReturnType<typeof seedArea>>) => string;
  heading: string;
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
    name: "the new agent wizard",
    href: (a) => `${a.agents}/new`,
    heading: "New agent",
  },
];

test.describe("agents on a phone", () => {
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
        () =>
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      );
      expect(overflow).toBeLessThanOrEqual(0);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
