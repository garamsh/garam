import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { createOrg } from "@/features/orgs/api/create-org";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `redirect()` throws a control-flow error the router catches; the
// test only needs to observe where it was sent.
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` only works inside a Next.js request store.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

function form(entries: Record<string, string>): FormData {
  const data = new FormData();
  for (const [key, value] of Object.entries(entries)) data.set(key, value);
  return data;
}

const created = {
  grn: "grn:root:default:org:acme2",
  name: "Acme 2",
  createdAt: "2026-01-02T03:04:05Z",
};

describe("createOrg", () => {
  it("posts the name and lands on the new organization's page", async () => {
    let body: unknown;
    server.use(
      http.post(`${API_URL}/orgs`, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(created, { status: 201 });
      }),
    );

    await expect(
      createOrg({ status: "idle" }, form({ name: "Acme 2" })),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme2");
    expect(body).toEqual({ name: "Acme 2" });
  });

  it("rejects an empty name without calling the backend", async () => {
    const state = await createOrg({ status: "idle" }, form({ name: "  " }));

    expect(state).toEqual({
      status: "error",
      message: "Give the organization a name.",
    });
  });

  it("surfaces the backend's error message", async () => {
    server.use(
      http.post(`${API_URL}/orgs`, () =>
        HttpResponse.json(
          { kind: "invalid", message: "name is required" },
          { status: 400 },
        ),
      ),
    );

    const state = await createOrg({ status: "idle" }, form({ name: "x" }));

    expect(state).toEqual({ status: "error", message: "name is required" });
  });
});
