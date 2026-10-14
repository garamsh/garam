import { axeViolations, expectHydrated } from "./axe";
import { expect, test } from "./fixtures";

// The Operators area at 390px: each route fits the screen and passes
// axe.
test.describe("operators on a phone", () => {
  for (const [route, path] of [
    ["the list", ""],
    ["an operator", "/OPERATOR"],
    ["an operator's keys", "/OPERATOR?tab=keys"],
    ["an operator's setup", "/OPERATOR/connect"],
  ] as const) {
    test(`${route} fits the screen and passes axe`, async ({
      page,
      signedIn,
    }) => {
      await page.goto(
        `/console/orgs/${signedIn.orgSlug}/operators${path.replace(
          "OPERATOR",
          signedIn.operatorIdentifier,
        )}`,
      );
      await expectHydrated(page);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();

      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow).toBeLessThanOrEqual(0);
      expect(await axeViolations(page)).toEqual([]);
    });
  }
});
