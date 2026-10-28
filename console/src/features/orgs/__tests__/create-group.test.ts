import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { createGroup } from "@/features/orgs/api/create-group";
import { API_URL, engineering } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

// `revalidatePath` only works inside a Next.js request store.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const GROUPS = `${API_URL}/orgs/acme/groups`;

function form(name: string): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("name", name);
  return data;
}

describe("createGroup", () => {
  it("posts the trimmed name and opens the team garam recorded", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(GROUPS, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(engineering, { status: 201 });
      }),
    );

    await expect(
      createGroup({ status: "idle" }, form("  Engineering  ")),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme/teams/eng");
    expect(posted).toEqual([{ name: "Engineering" }]);
  });

  it("refuses a nameless team without asking garam", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(GROUPS, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(engineering, { status: 201 });
      }),
    );

    await expect(createGroup({ status: "idle" }, form("  "))).resolves.toEqual({
      status: "error",
      message: "Give the team a name.",
    });
    expect(posted).toEqual([]);

    // The control: the same form carrying a name reaches garam, so the
    // refusal above is the empty name and not a form read as nothing.
    await expect(
      createGroup({ status: "idle" }, form("Engineering")),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme/teams/eng");
    expect(posted).toEqual([{ name: "Engineering" }]);
  });

  it("renders the refusal garam answered rather than throwing", async () => {
    let refusing = true;
    server.use(
      http.post(GROUPS, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : HttpResponse.json(engineering, { status: 201 }),
      ),
    );

    await expect(
      createGroup({ status: "idle" }, form("Engineering")),
    ).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.permissionDenied,
    });

    // The control: the same submission garam accepts opens the team,
    // so the error above is garam's refusal and not the form's.
    refusing = false;
    await expect(
      createGroup({ status: "idle" }, form("Engineering")),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme/teams/eng");
  });
});
