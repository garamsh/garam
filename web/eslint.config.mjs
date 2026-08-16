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

// The import direction of stack-nextjs.md §12: the cross-feature tiers
// import neither features nor routes, a feature imports neither routes
// nor a sibling, and routes import both. `app` reaches a feature only
// through the two files the feature declares public: `index.ts`, the
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
    ...features.flatMap((name) => [
      {
        target: `./src/features/${name}`,
        from: "./src/features",
        except: [`./${name}`],
        message: "A feature may not import another feature.",
      },
      {
        target: "./src/app",
        from: `./src/features/${name}`,
        except: ["./index.ts", "./actions.ts"],
        message: `Import ${name} through its index.ts or actions.ts.`,
      },
    ]),
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
