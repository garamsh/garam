import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { deleteGroup } from "@/features/orgs/api/delete-group";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";

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
    let refusing = true;
    server.use(
      http.delete(GROUP, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : new HttpResponse(null, { status: 204 }),
      ),
    );

    await expect(deleteGroup({ status: "idle" }, form())).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.permissionDenied,
    });

    // The control: the same delete garam accepts returns to the list,
    // so staying above is garam's refusal.
    refusing = false;
    await expect(deleteGroup({ status: "idle" }, form())).rejects.toThrow(
      "NEXT_REDIRECT:/console/orgs/acme/teams",
    );
  });
});
