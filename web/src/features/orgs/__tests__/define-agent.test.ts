import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { defineAgent } from "@/features/orgs/api/define-agent";
import { API_URL, fieldAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` only works inside a Next.js request store.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

/** Where a defined agent sends the person: its own page. */
const OPENED = "NEXT_REDIRECT:/console/orgs/acme/agents/9f2ac1b40d8e7a35";

const DEFINITIONS = `${API_URL}/orgs/acme/agent-definitions`;

/** A submission of the define form: one `key` and one `value` field per row. */
function form(keys: readonly string[], values: readonly string[]): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("name", "Scout");
  data.set("operator", "field-one");
  for (const key of keys) data.append("key", key);
  for (const value of values) data.append("value", value);
  return data;
}

const paired = form(["model", "system-prompt"], ["haiku", "be brief"]);

describe("defineAgent", () => {
  it("sends each key under the value submitted beside it", async () => {
    let body: unknown;
    server.use(
      http.post(DEFINITIONS, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(fieldAgent, { status: 201 });
      }),
    );

    await expect(defineAgent({ status: "idle" }, paired)).rejects.toThrow(
      OPENED,
    );
    expect(body).toEqual({
      name: "Scout",
      operatorGrn: "grn:acme:default:operator:field-one",
      values: { model: "haiku", "system-prompt": "be brief" },
    });
  });

  it.each<[string, string[], string[]]>([
    ["a key with no value", ["model", "system-prompt"], ["haiku"]],
    ["a value with no key", ["model"], ["haiku", "be brief"]],
  ])("refuses a submission carrying %s", async (_shape, keys, values) => {
    let posts = 0;
    server.use(
      http.post(DEFINITIONS, () => {
        posts += 1;
        return HttpResponse.json(fieldAgent, { status: 201 });
      }),
    );

    await expect(
      defineAgent({ status: "idle" }, form(keys, values)),
    ).resolves.toEqual({
      status: "error",
      message: "The form did not arrive complete.",
    });
    expect(posts).toBe(0);

    // The control: the same rows with both halves present are defined,
    // so the refusal above is the missing half and not a form the
    // action reads nothing from.
    await expect(defineAgent({ status: "idle" }, paired)).rejects.toThrow(
      OPENED,
    );
  });
});
