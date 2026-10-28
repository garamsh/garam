import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

const css = readFileSync(join(import.meta.dirname, "../globals.css"), "utf8");

/** The custom properties one block of globals.css declares. */
function block(selector: string): Map<string, string> {
  const start = css.indexOf(`${selector} {`);
  expect(start, `globals.css declares ${selector}`).toBeGreaterThanOrEqual(0);
  const body = css.slice(start, css.indexOf("\n}", start));
  return new Map(
    [...body.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2]]),
  );
}

type Rgb = [number, number, number];

/** An `oklch(L C h)` token as linear sRGB, clamped to the gamut. */
function linear(token: string): Rgb {
  const match = /^oklch\(([\d.]+) ([\d.]+) ([\d.]+)\)$/.exec(token.trim());
  expect(match, `${token} is an opaque oklch() colour`).not.toBeNull();
  const [L, C, h] = match!.slice(1).map(Number);
  const a = C * Math.cos((h * Math.PI) / 180);
  const b = C * Math.sin((h * Math.PI) / 180);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const clamp = (x: number) => Math.min(Math.max(x, 0), 1);
  return [
    clamp(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    clamp(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    clamp(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  ];
}

/** sRGB's transfer function, between linear light and the encoded value. */
const encode = (x: number) =>
  x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055;
const decode = (x: number) =>
  x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;

/** `top` at `alpha` over `bottom`, blended as a browser composites. */
function over(top: Rgb, bottom: Rgb, alpha: number): Rgb {
  return top.map((value, i) =>
    decode(alpha * encode(value) + (1 - alpha) * encode(bottom[i])),
  ) as Rgb;
}

const luminance = ([r, g, b]: Rgb) => 0.2126 * r + 0.7152 * g + 0.0722 * b;

/** WCAG 2.2 contrast ratio between two linear colours. */
function ratio(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/** WCAG 2.2 contrast ratio between two colours. */
function contrast(a: string, b: string): number {
  const luminance = ([r, g, bl]: Rgb) => 0.2126 * r + 0.7152 * g + 0.0722 * bl;
  const [hi, lo] = [luminance(linear(a)), luminance(linear(b))].sort(
    (x, y) => y - x,
  );
  return (hi + 0.05) / (lo + 0.05);
}

const THEMES = { light: ":root", dark: ".dark" } as const;

describe("theme tokens", () => {
  for (const [theme, selector] of Object.entries(THEMES)) {
    describe(theme, () => {
      const tokens = block(selector);
      const get = (name: string) => {
        const value = tokens.get(name);
        expect(value, `${selector} declares ${name}`).toBeDefined();
        return value!;
      };

      // WCAG 2.2 SC 1.4.11: a control's boundary and a graphic carrying
      // meaning hold 3:1 against what they sit on.
      it("draws control borders at 3:1 against the page and muted fills", () => {
        for (const surface of ["--background", "--muted", "--card"]) {
          expect(contrast(get("--input"), get(surface))).toBeGreaterThanOrEqual(
            3,
          );
        }
      });

      it("draws every status colour at 3:1 against the page and the sidebar", () => {
        for (const status of ["--ok", "--warn", "--danger", "--info"]) {
          for (const surface of ["--background", "--sidebar"]) {
            expect(
              contrast(get(status), get(surface)),
              `${status} on ${surface}`,
            ).toBeGreaterThanOrEqual(3);
          }
        }
      });

      it("keeps body and muted text at 4.5:1", () => {
        for (const surface of ["--background", "--sidebar", "--muted"]) {
          expect(
            contrast(get("--foreground"), get(surface)),
          ).toBeGreaterThanOrEqual(4.5);
          expect(
            contrast(get("--muted-foreground"), get(surface)),
          ).toBeGreaterThanOrEqual(4.5);
        }
      });

      it("declares danger once, with no separate destructive colour", () => {
        expect(tokens.has("--danger")).toBe(true);
        expect(tokens.has("--destructive")).toBe(false);
      });
    });
  }

  it("maps the destructive utility onto the danger token", () => {
    expect(css).toMatch(/--color-destructive:\s*var\(--danger\);/);
  });

  // The destructive button draws danger text on a danger tint: 10% over
  // the page in light, 20% in dark (`components/ui/button.tsx`).
  for (const [theme, selector, tint] of [
    ["light", ":root", 0.1],
    ["dark", ".dark", 0.2],
  ] as const) {
    it(`keeps danger text at 4.5:1 on the destructive button in ${theme}`, () => {
      const tokens = block(selector);
      const danger = linear(tokens.get("--danger")!);
      const page = linear(tokens.get("--background")!);

      expect(ratio(danger, over(danger, page, tint))).toBeGreaterThanOrEqual(
        4.5,
      );
    });
  }
});
