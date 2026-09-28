import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { createAgentTemplate } from "@/features/orgs/api/create-agent-template";
import { deleteAgentTemplate } from "@/features/orgs/api/delete-agent-template";
import { updateAgentTemplate } from "@/features/orgs/api/update-agent-template";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const TEMPLATES = `${API_URL}/orgs/acme/agent-templates`;

const researcher = {
  grn: "grn:acme:default:agent-template:7c1e2a9b3d4f5a60",
  orgGrn: "grn:root:default:org:acme",
  name: "Researcher",
  description: "Reads papers",
  values: { workspace: "papers" },
  createdBy: "grn:root:default:user:alice",
  createdAt: "2026-09-28T00:00:00Z",
  updatedAt: "2026-09-28T00:00:00Z",
};

/** A submission of the template form. */
function form(fields: Record<string, string>, rows: [string, string][]) {
  const data = new FormData();
  data.set("org", "acme");
  for (const [name, value] of Object.entries(fields)) data.set(name, value);
  for (const [key, value] of rows) {
    data.append("key", key);
    data.append("value", value);
  }
  return data;
}

/** garam's refusal; a response body is read once, so each use builds one. */
function refusal() {
  return HttpResponse.json(
    { kind: "permission_denied", message: "permission denied" },
    { status: 403 },
  );
}

describe("createAgentTemplate", () => {
  it("sends the trimmed fields and opens the created template", async () => {
    let body: unknown;
    server.use(
      http.post(TEMPLATES, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(researcher, { status: 201 });
      }),
    );

    await expect(
      createAgentTemplate(
        { status: "idle" },
        form({ name: " Researcher ", description: "Reads papers" }, [
          ["workspace", "papers"],
        ]),
      ),
    ).rejects.toThrow(
      "NEXT_REDIRECT:/console/orgs/acme/agents/templates/7c1e2a9b3d4f5a60",
    );
    expect(body).toEqual({
      name: "Researcher",
      description: "Reads papers",
      values: { workspace: "papers" },
    });
  });

  it("refuses an empty name without a round trip", async () => {
    let called = false;
    server.use(
      http.post(TEMPLATES, () => {
        called = true;
        return HttpResponse.json(researcher, { status: 201 });
      }),
    );

    const state = await createAgentTemplate(
      { status: "idle" },
      form({ name: "  ", description: "" }, []),
    );

    expect(state).toEqual({
      status: "error",
      message: "Give the template a name.",
    });
    expect(called).toBe(false);
  });

  it("renders garam's refusal rather than throwing", async () => {
    server.use(http.post(TEMPLATES, refusal));

    const state = await createAgentTemplate(
      { status: "idle" },
      form({ name: "Researcher", description: "" }, []),
    );

    expect(state).toEqual({ status: "error", message: "permission denied" });
  });
});

describe("updateAgentTemplate", () => {
  it("replaces the one template's fields and says it was saved", async () => {
    const puts: { template: string; body: unknown }[] = [];
    server.use(
      http.put(`${TEMPLATES}/:template`, async ({ params, request }) => {
        puts.push({
          template: String(params.template),
          body: await request.json(),
        });
        return HttpResponse.json({ ...researcher, name: "Analyst" });
      }),
    );

    const state = await updateAgentTemplate(
      { status: "idle" },
      form({ template: "7c1e2a9b3d4f5a60", name: "Analyst", description: "" }, [
        ["workspace", "archive"],
      ]),
    );

    expect(state).toEqual({ status: "saved", name: "Analyst" });
    expect(puts).toEqual([
      {
        template: "7c1e2a9b3d4f5a60",
        body: {
          name: "Analyst",
          description: "",
          values: { workspace: "archive" },
        },
      },
    ]);
  });
});

describe("deleteAgentTemplate", () => {
  it("deletes the one template and returns to the list", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(`${TEMPLATES}/:template`, ({ params }) => {
        deleted.push(String(params.template));
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      deleteAgentTemplate(
        { status: "idle" },
        form({ template: "7c1e2a9b3d4f5a60" }, []),
      ),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme/agents/templates");
    expect(deleted).toEqual(["7c1e2a9b3d4f5a60"]);
  });

  it("stays on the template and says why when garam refuses", async () => {
    server.use(http.delete(`${TEMPLATES}/:template`, refusal));

    const state = await deleteAgentTemplate(
      { status: "idle" },
      form({ template: "7c1e2a9b3d4f5a60" }, []),
    );

    expect(state).toEqual({ status: "error", message: "permission denied" });
  });
});
