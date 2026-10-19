import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";
import { seedDefinition, type Seed } from "./seed";

/** What the area's routes read, seeded into the test's organization. */
async function seedArea(signedIn: Seed) {
  const defined = await seedDefinition(signedIn, "Moving");
  return { agents: `/console/orgs/${signedIn.orgSlug}/agents`, defined };
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
    href: (a) => `${a.agents}/${a.defined}`,
    heading: "Moving",
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
