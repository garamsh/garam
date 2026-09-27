import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { updateGroup } from "@/features/orgs/api/update-group";
import { API_URL, engineering } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const GROUP = `${API_URL}/orgs/acme/groups/eng`;

function form(name: string): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("group", "eng");
  data.set("name", name);
  return data;
}

describe("updateGroup", () => {
  it("sends the name alone, leaving the roles as they stand", async () => {
    const patched: unknown[] = [];
    server.use(
      http.patch(GROUP, async ({ request }) => {
        patched.push(await request.json());
        return HttpResponse.json({ ...engineering, name: "Platform" });
      }),
    );

    await expect(
      updateGroup({ status: "idle" }, form("Platform")),
    ).resolves.toEqual({ status: "saved", name: "Platform" });
    expect(patched).toEqual([{ name: "Platform" }]);
  });

  it("renders a refusal in place of the saved name", async () => {
    server.use(
      http.patch(GROUP, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );

    await expect(
      updateGroup({ status: "idle" }, form("Platform")),
    ).resolves.toEqual({ status: "error", message: "permission denied" });
  });
});
