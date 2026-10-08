import { readdirSync } from "node:fs";
import { join } from "node:path";

import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

// Read from disk so a feature added later is covered without editing
// this file — an unlisted feature would silently drop out of the rule.
const features = readdirSync(join(import.meta.dirname, "src/features"), {
  withFileTypes: true,
})
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name);

// What may import what under src/ is structure.md §Boundaries and
// dependency direction: shared code imports neither a feature nor a
// route, a feature imports no route, and a route or a sibling feature
// reaches a feature only through the two files stack-nextjs.md §13
// Feature entry points and file naming has it declare: `index.ts`, the
// server-only read surface, and `actions.ts`, the Server Actions — kept
// apart so a client module cannot pull the backend client into the
// browser bundle.
const importDirection = {
  basePath: import.meta.dirname,
  zones: [
    {
      target: ["./src/components", "./src/hooks", "./src/lib", "./src/types"],
      from: ["./src/app", "./src/features"],
      message: "Cross-feature code may not import a feature or a route.",
    },
    {
      target: "./src/features",
      from: "./src/app",
      message: "A feature may not import a route.",
    },
    ...features.map((name) => ({
      target: [
        "./src/app",
        ...features
          .filter((sibling) => sibling !== name)
          .map((sibling) => `./src/features/${sibling}`),
      ],
      from: `./src/features/${name}`,
      except: ["./index.ts", "./actions.ts"],
      message: `Import ${name} through its index.ts or actions.ts.`,
    })),
  ],
};

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    name: "garam/import-direction",
    rules: {
      "import/no-restricted-paths": ["error", importDirection],
    },
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
