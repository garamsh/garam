import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { updateGroup } from "@/features/orgs/api/update-group";
import { API_URL, engineering } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";

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
    let refusing = true;
    server.use(
      http.patch(GROUP, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : HttpResponse.json({ ...engineering, name: "Platform" }),
      ),
    );

    await expect(
      updateGroup({ status: "idle" }, form("Platform")),
    ).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.permissionDenied,
    });

    // The control: the same rename garam accepts is saved, so the
    // error above is garam's refusal.
    refusing = false;
    await expect(
      updateGroup({ status: "idle" }, form("Platform")),
    ).resolves.toEqual({ status: "saved", name: "Platform" });
  });
});
