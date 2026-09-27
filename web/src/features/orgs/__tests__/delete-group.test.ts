import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { deleteGroup } from "@/features/orgs/api/delete-group";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const GROUP = `${API_URL}/orgs/acme/groups/eng`;

function form(): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("group", "eng");
  return data;
}

describe("deleteGroup", () => {
  it("deletes the one team and returns to the list", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(`${API_URL}/orgs/acme/groups/:group`, ({ params }) => {
        deleted.push(String(params.group));
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(deleteGroup({ status: "idle" }, form())).rejects.toThrow(
      "NEXT_REDIRECT:/console/orgs/acme/teams",
    );
    expect(deleted).toEqual(["eng"]);
  });

  it("stays on the team and says why when garam refuses", async () => {
    server.use(
      http.delete(GROUP, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );

    await expect(deleteGroup({ status: "idle" }, form())).resolves.toEqual({
      status: "error",
      message: "permission denied",
    });
  });
});
