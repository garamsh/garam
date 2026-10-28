import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

// Every app compiles this package's source with its own `@/` alias, so
// an `@/` import here would resolve inside whichever app imported it.
const noAppAlias = {
  patterns: [
    {
      group: ["@/*"],
      message:
        "design/ is compiled by each app: import its own files relatively.",
    },
  ],
};

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    rules: {
      "no-restricted-imports": ["error", noAppAlias],
      // The rule reads a pages/ folder, which a package of components
      // has none of.
      "@next/next/no-html-link-for-pages": "off",
    },
  },
  globalIgnores(["coverage/**"]),
]);
