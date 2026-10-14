import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";

/** The WCAG levels the console is held to. */
const WCAG = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];

/**
 * Holds every element at its end state while axe reads it. A control
 * that becomes enabled during a scan still shows the colours it is
 * transitioning from, so its contrast would be measured between two
 * states rather than in either (#1415). Cancelling a running
 * transition puts its property at the value it was moving to.
 */
const STILL =
  "*, *::before, *::after { transition: none !important; animation: none !important; }";

/**
 * Runs axe over the page, or over the parts `include` selects, and
 * returns each violation as rule id and the nodes it found, which is
 * what a failing assertion should print. Nothing moves while it runs.
 */
export async function axeViolations(
  page: Page,
  include: readonly string[] = [],
): Promise<{ id: string; nodes: string[] }[]> {
  let builder = new AxeBuilder({ page }).withTags(WCAG);
  for (const selector of include) builder = builder.include(selector);
  const still = await page.addStyleTag({ content: STILL });
  try {
    const { violations } = await builder.analyze();
    return violations.map((violation) => ({
      id: violation.id,
      nodes: violation.nodes.map((node) => node.target.join(" ")),
    }));
  } finally {
    await still.evaluate((sheet) => {
      if (!(sheet instanceof Element)) throw new Error("not a style tag");
      sheet.remove();
    });
  }
}

/** Starts every page the test opens in the theme given. */
export async function useTheme(page: Page, theme: "light" | "dark") {
  await page.addInitScript((value) => {
    window.localStorage.setItem("garam-theme", value);
  }, theme);
}

/**
 * Waits until React has hydrated the console shell: the palette's
 * control is disabled until then, so a scan that follows it reads the
 * page as a visitor can use it.
 */
export async function expectHydrated(page: Page) {
  await expect(
    page.getByRole("button", { name: /Search or jump to/ }),
  ).toBeEnabled();
}
