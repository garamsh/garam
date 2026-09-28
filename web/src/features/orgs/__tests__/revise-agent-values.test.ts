import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { reviseAgentValues } from "@/features/orgs/api/revise-agent-values";
import { API_URL, fieldAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` only works inside a Next.js request store.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ONE = `${API_URL}/orgs/acme/agent-definitions/9f2ac1b40d8e7a35`;

/** A submission of the settings form: one `key` and `value` per row. */
function form(
  expected: string,
  keys: readonly string[],
  values: readonly string[],
): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("agent", "9f2ac1b40d8e7a35");
  data.set("expectedRevision", expected);
  for (const key of keys) data.append("key", key);
  for (const value of values) data.append("value", value);
  return data;
}

const idle = { status: "idle" } as const;

describe("reviseAgentValues", () => {
  it("saves the values against the revision the form read", async () => {
    let body: unknown;
    server.use(
      http.put(`${ONE}/values`, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({
          ...fieldAgent,
          revision: 2,
          values: { model: "sonnet" },
        });
      }),
    );

    await expect(
      reviseAgentValues(idle, form("1", ["model"], ["sonnet"])),
    ).resolves.toEqual({ status: "saved", revision: 2 });
    expect(body).toEqual({
      expectedRevision: 1,
      values: { model: "sonnet" },
    });
  });

  it("answers a conflict with the settings another save left", async () => {
    server.use(
      http.put(`${ONE}/values`, () =>
        HttpResponse.json(
          { kind: "aborted", message: "agent definition revision has changed" },
          { status: 409 },
        ),
      ),
      http.get(ONE, () =>
        HttpResponse.json({
          ...fieldAgent,
          revision: 3,
          values: { model: "opus" },
        }),
      ),
    );

    await expect(
      reviseAgentValues(idle, form("1", ["model"], ["sonnet"])),
    ).resolves.toEqual({
      status: "conflict",
      latest: { revision: 3, values: { model: "opus" } },
    });
  });

  it("still answers a conflict when the saved settings cannot be read", async () => {
    server.use(
      http.put(`${ONE}/values`, () =>
        HttpResponse.json(
          { kind: "aborted", message: "agent definition revision has changed" },
          { status: 409 },
        ),
      ),
      http.get(ONE, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await expect(
      reviseAgentValues(idle, form("1", ["model"], ["sonnet"])),
    ).resolves.toEqual({ status: "conflict", latest: null });
  });

  it("answers any other refusal with garam's message, not a conflict", async () => {
    server.use(
      http.put(`${ONE}/values`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "insufficient role" },
          { status: 403 },
        ),
      ),
    );

    const answer = await reviseAgentValues(idle, form("1", ["model"], ["x"]));
    expect(answer.status).toBe("error");
  });

  it.each<[string, FormData]>([
    ["a key outside the character set", form("1", ["Model"], ["haiku"])],
    ["a key with no value", form("1", ["model", "notes"], ["haiku"])],
    ["no revision", form("", ["model"], ["haiku"])],
  ])("refuses %s before asking garam", async (_shape, refused) => {
    let puts = 0;
    server.use(
      http.put(`${ONE}/values`, () => {
        puts += 1;
        return HttpResponse.json({ ...fieldAgent, revision: 2 });
      }),
    );

    expect((await reviseAgentValues(idle, refused)).status).toBe("error");
    expect(puts).toBe(0);

    // The control: a well-formed submission reaches garam.
    await expect(
      reviseAgentValues(idle, form("1", ["model"], ["haiku"])),
    ).resolves.toEqual({ status: "saved", revision: 2 });
    expect(puts).toBe(1);
  });
});
