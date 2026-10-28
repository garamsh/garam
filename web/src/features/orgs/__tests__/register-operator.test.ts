import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { registerOperator } from "@/features/orgs/api/register-operator";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
import { API_URL, fieldOne } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function form(fields: Record<string, string>): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("name", "");
  data.set("description", "");
  for (const [key, value] of Object.entries(fields)) data.set(key, value);
  return data;
}

describe("registerOperator", () => {
  it("registers the identifier alone and opens the operator's setup, where its key is issued", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(`${API_URL}/orgs/acme/operators`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(fieldOne, { status: 201 });
      }),
    );

    await expect(
      registerOperator({ status: "idle" }, form({ identifier: "field-one" })),
    ).rejects.toThrow(
      "NEXT_REDIRECT:/console/orgs/acme/operators/field-one/connect",
    );
    // No name is sent, so the API calls the operator by its identifier
    // (ADR-0074).
    expect(posted).toEqual([{ identifier: "field-one", description: "" }]);
  });

  it("answers a refusal in garam's words and opens nothing", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "operator exists" },
          { status: 409 },
        ),
      ),
    );

    await expect(
      registerOperator({ status: "idle" }, form({ identifier: "field-one" })),
    ).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.alreadyExists,
    });
  });

  it("registers a cluster manager with its control service in one request, and opens the pair's setup (#1636)", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(`${API_URL}/orgs/acme/operators`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(fieldOne, { status: 201 });
      }),
    );

    await expect(
      registerOperator(
        { status: "idle" },
        form({
          identifier: "field-one",
          manager: "",
          controlService: "field-one-control",
        }),
      ),
    ).rejects.toThrow(
      "NEXT_REDIRECT:/console/orgs/acme/operators/field-one/connect",
    );
    expect(posted).toEqual([
      {
        identifier: "field-one",
        description: "",
        controlService: { identifier: "field-one-control" },
      },
    ]);
  });

  it("refuses a control service identifier outside the character rule without a request", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(`${API_URL}/orgs/acme/operators`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(fieldOne, { status: 201 });
      }),
    );

    const answer = await registerOperator(
      { status: "idle" },
      form({ identifier: "field-one", controlService: "Field One" }),
    );

    expect(answer.status).toBe("error");
    expect(posted).toEqual([]);
  });
});
