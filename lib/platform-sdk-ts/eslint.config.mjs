import { defineConfig, globalIgnores } from "eslint/config";
import nextTs from "eslint-config-next/typescript";

// Every app compiles this package's source with its own `@/` alias, so
// an `@/` import here would resolve inside whichever app imported it.
const noAppAlias = {
  patterns: [
    {
      group: ["@/*"],
      message:
        "lib/platform-sdk-ts is compiled by each app: import its own files relatively.",
    },
  ],
};

export default defineConfig([
  ...nextTs,
  { rules: { "no-restricted-imports": ["error", noAppAlias] } },
  globalIgnores(["coverage/**", "src/api.ts"]),
]);
