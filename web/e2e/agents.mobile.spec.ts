import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";
import { seedArchivedTemplate, seedCutoverDefinition } from "./seed";

// The Agents and Templates routes at 390px: a table scrolls inside its
// own container, never the page.
test.describe("agents and templates on a phone", () => {
  test("every route scrolls no wider than the screen and passes axe", async ({
    page,
    signedIn,
  }) => {
    const defined = await seedCutoverDefinition(signedIn, "Moving", "frozen");
    const kept = await seedArchivedTemplate(signedIn, "Researcher", {
      workspace: "papers",
    });
    const agents = `/console/orgs/${signedIn.orgSlug}/agents`;

    for (const [href, heading] of [
      [agents, "Agents"],
      [`${agents}/${defined}`, "Moving"],
      [`${agents}/new`, "New agent"],
      [`${agents}/templates`, "Templates"],
      [`${agents}/templates/${kept.identifier}`, "Researcher"],
    ] as const) {
      await page.goto(href);
      await expect(
        page.getByRole("heading", { level: 1, name: heading, exact: true }),
      ).toBeVisible();
      await expectHydrated(page);

      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow, href).toBeLessThanOrEqual(0);
      expect(await axeViolations(page), href).toEqual([]);
    }
  });
});
