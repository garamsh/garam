import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { createInstance } from "@/features/orgs/api/create-instance";
import { installApp } from "@/features/orgs/api/install-app";
import { reissueInstanceToken } from "@/features/orgs/api/reissue-instance-token";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const INSTALLATION = `${API_URL}/orgs/acme/installations/garam-bot-connector`;

function form(fields: Record<string, string | string[]>): FormData {
  const data = new FormData();
  data.set("org", "acme");
  for (const [key, value] of Object.entries(fields)) {
    for (const each of [value].flat()) data.append(key, each);
  }
  return data;
}

describe("installApp", () => {
  it("refuses a project scope with no project, and sends nothing", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(`${API_URL}/orgs/acme/installations`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json({}, { status: 201 });
      }),
    );

    await expect(
      installApp(
        { status: "idle" },
        form({ app: "garam-bot-connector", scope: "projects" }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: "Choose at least one project.",
    });
    expect(posted).toEqual([]);
  });

  it("answers garam's refusal in garam's words", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/installations`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "installed" },
          { status: 409 },
        ),
      ),
    );

    await expect(
      installApp(
        { status: "idle" },
        form({ app: "garam-bot-connector", scope: "organization" }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: REFUSAL_COPY.alreadyExists,
    });
  });
});

describe("createInstance", () => {
  it("leaves out a field left empty, so garam names the instance by its identifier", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(`${INSTALLATION}/instances`, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(
          {
            instance: {
              grn: "grn:acme/iam/instance/i-1",
              installationGrn: "grn:acme/iam/installation/garam-bot-connector",
              name: "i-1",
              description: "",
              kind: "",
              orgGrn: "grn:root/iam/org/acme",
              createdBy: "grn:root/iam/user/alice",
              createdAt: "2026-11-03T08:00:00Z",
            },
            registration: {
              registrationToken: "t",
              expiresAt: "2026-11-03T09:00:00Z",
            },
          },
          { status: 201 },
        );
      }),
    );

    await expect(
      createInstance(
        { status: "idle" },
        form({
          installation: "garam-bot-connector",
          name: "",
          description: "",
          kind: "",
        }),
      ),
    ).resolves.toEqual({
      status: "created",
      name: "i-1",
      registrationToken: "t",
      expiresAt: "2026-11-03T09:00:00Z",
    });
    expect(posted).toEqual([{}]);
  });

  it("answers the refusal for an app that runs no instance", async () => {
    server.use(
      http.post(`${INSTALLATION}/instances`, () =>
        HttpResponse.json(
          { kind: "not_found", message: "no instances" },
          { status: 404 },
        ),
      ),
    );

    const answer = await createInstance(
      { status: "idle" },
      form({ installation: "garam-bot-connector" }),
    );

    expect(answer.status).toBe("error");
  });
});

describe("reissueInstanceToken", () => {
  it("answers the new token for the page to show once", async () => {
    server.use(
      http.post(`${INSTALLATION}/instances/i-1/registration-token`, () =>
        HttpResponse.json({
          registrationToken: "again",
          expiresAt: "2026-11-03T09:00:00Z",
        }),
      ),
    );

    await expect(
      reissueInstanceToken(
        { status: "idle" },
        form({ installation: "garam-bot-connector", instance: "i-1" }),
      ),
    ).resolves.toEqual({
      status: "reissued",
      registrationToken: "again",
      expiresAt: "2026-11-03T09:00:00Z",
    });
  });
});
