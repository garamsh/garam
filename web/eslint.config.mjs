import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // The Playwright fixture callback takes a parameter named `use`,
  // which the React Hooks rule reads as the `use` hook. No React runs
  // in the e2e suite.
  {
    files: ["e2e/**"],
    rules: { "react-hooks/rules-of-hooks": "off" },
  },
  // shadcn CLI output. `pnpm dlx shadcn@latest add` rewrites these
  // files verbatim, so a fix applied here does not survive the next
  // regeneration; they are read as vendor code, not as ours.
  {
    files: ["src/components/ui/**", "src/hooks/use-mobile.ts"],
    rules: { "react-hooks/set-state-in-effect": "off" },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    "test-results/**",
    "playwright-report/**",
  ]),
]);

export default eslintConfig;
