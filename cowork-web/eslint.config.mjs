import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

// The plugin boundary (ADR-0127 §Decision 9): the editor and the pages
// reach blocks only through the registry, so no module outside a
// plugin imports a TipTap extension, and a plugin reaches no route or
// editor internals.
const pluginBoundary = {
  basePath: import.meta.dirname,
  zones: [
    {
      target: "./src/plugins",
      from: ["./src/app", "./src/features"],
      message: "A plugin reaches the app only through the registry's API.",
    },
  ],
};

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    rules: {
      "import/no-restricted-paths": ["error", pluginBoundary],
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: [
                "@tiptap/extension-*",
                "!@tiptap/extension-collaboration",
                "!@tiptap/extension-collaboration-caret",
              ],
              message:
                "Block types are plugins: import an extension inside src/plugins/ and register it.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/plugins/**"],
    rules: { "no-restricted-imports": "off" },
  },
  globalIgnores([
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    "src/types/api.ts",
  ]),
]);
