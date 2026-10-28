import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The `.ts` tests that reach the DOM. Every other `.ts` test runs in
// Node, where `document` and `window` are undefined, so one that starts
// reaching the DOM fails with a ReferenceError until it is listed here.
const domTs = [
  "src/hooks/__tests__/use-mobile.test.ts",
  "src/lib/__tests__/theme.test.ts",
];

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    globals: true,
    // Measured on the 12-core gate host (#1528): from 4 workers up, a
    // loaded host timed out the 5 s budget, and at 3 none did. On a
    // 4-core hosted runner 3 is the default.
    maxWorkers: 3,
    // Playwright owns e2e/; Vitest owns the colocated tests under src/.
    // A component test pays for jsdom; a pure `.ts` test does not.
    projects: [
      {
        extends: true,
        test: {
          name: "node",
          environment: "node",
          setupFiles: ["./src/testing/setup.ts"],
          include: ["src/**/*.test.ts"],
          exclude: domTs,
        },
      },
      {
        extends: true,
        test: {
          name: "dom",
          environment: "jsdom",
          setupFiles: ["./src/testing/setup.ts", "./src/testing/setup-dom.ts"],
          include: ["src/**/*.test.tsx", ...domTs],
        },
      },
    ],
  },
});
