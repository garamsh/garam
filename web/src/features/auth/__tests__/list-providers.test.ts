import { describe, expect, it } from "vitest";

import { listProviders } from "@/features/auth/api/list-providers";

describe("listProviders", () => {
  it("returns the provider names the backend advertises", async () => {
    await expect(listProviders()).resolves.toEqual(["google"]);
  });
});
