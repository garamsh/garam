import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { createOrg } from "@/features/orgs/api/create-org";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { unmappedRefusalCopy } from "@garam/platform-client/refusal-copy";

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

  it("refuses a GRN it cannot read where the request is made", async () => {
    server.use(
      http.post(`${API_URL}/orgs`, () =>
        HttpResponse.json(
          { ...created, grn: "root:default:org:acme2" },
          { status: 201 },
        ),
      ),
    );

    // Not "malformed GRN": the identifier the redirect is built from is
    // read at the seam, so nothing between the write and the redirect
    // is left to throw over a body the console cannot read.
    await expect(
      createOrg({ status: "idle" }, form({ name: "Acme 2" })),
    ).rejects.toThrow(/outside its specification/);

    // The control: the same write against the body the API answers with
    // redirects, so the refusal above is the GRN and not a parser that
    // refuses every organization.
    server.use(
      http.post(`${API_URL}/orgs`, () =>
        HttpResponse.json(created, { status: 201 }),
      ),
    );

    await expect(
      createOrg({ status: "idle" }, form({ name: "Acme 2" })),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme2");
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
          { kind: "invalid_argument", message: "name is required" },
          { status: 400 },
        ),
      ),
    );

    const state = await createOrg({ status: "idle" }, form({ name: "x" }));

    expect(state).toEqual({
      status: "error",
      message: unmappedRefusalCopy("name is required"),
    });
  });
});
