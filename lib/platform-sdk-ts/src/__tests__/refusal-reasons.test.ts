import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { MAPPED_REASONS, UNMAPPED_REASONS } from "../refusal-copy";

/**
 * The codes `platform/api/openapi.yaml` documents under `Error.reason`,
 * read off the description `lib/platform-sdk-ts/src/api.ts` is generated
 * with — which `make -C lib/platform-sdk-ts check-gen` holds to the
 * specification — as every backticked snake_case word on the comment
 * above `reason?: string`.
 */
function documentedReasons(): string[] {
  const generated = readFileSync(
    join(import.meta.dirname, "../api.ts"),
    "utf8",
  );
  const comment =
    /\/\*\* @description ([^\n]*) \*\/\n\s*reason\?: string;/.exec(generated);
  if (comment === null) throw new Error("Error.reason carries no description");
  return [...comment[1].matchAll(/`([a-z]+(?:_[a-z]+)+)`/g)].map(
    (match) => match[1],
  );
}

describe("refusal reasons", () => {
  it("writes a sentence for every documented reason, or says it does not on purpose", () => {
    const documented = documentedReasons();
    // Read non-empty, so an empty pair cannot pass as agreeing.
    expect(documented.length).toBeGreaterThan(0);

    expect([...MAPPED_REASONS, ...UNMAPPED_REASONS].sort()).toEqual(
      [...documented].sort(),
    );
  });
});
