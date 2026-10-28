import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The same two projects as console's: a component test pays for jsdom,
// a pure `.ts` test does not, and a `.ts` test reaching the DOM is
// listed here.
const domTs = [
  "src/hooks/__tests__/use-mobile.test.ts",
  "src/lib/__tests__/theme.test.ts",
];

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    projects: [
      {
        extends: true,
        test: {
          name: "node",
          environment: "node",
          include: ["src/**/*.test.ts"],
          exclude: domTs,
        },
      },
      {
        extends: true,
        test: {
          name: "dom",
          environment: "jsdom",
          setupFiles: ["./src/testing/setup-dom.ts"],
          include: ["src/**/*.test.tsx", ...domTs],
        },
      },
    ],
  },
});
