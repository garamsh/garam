import { createHash } from "node:crypto";

import type { Page } from "@playwright/test";

import { expect, test } from "./fixtures";
import {
  recordedAuthority,
  seedHostedOperator,
  seedMemberDeniedOperatorRead,
  type Seed,
} from "./seed";

/*
 * The console against a hosted operator's control service (#1170).
 *
 * garam is real in every test here: the session, the operator record,
 * the delegation and every operation authority are minted by the
 * running API and read back from its database. The control service is
 * not. These are contract tests against the merged Operator wire
 * (garam-agent-operator `dev` 8e1116933408), answered by a stub at the
 * registered endpoint. They show what the console sends and that garam
 * bound exactly that, and nothing about a running control service.
 */

const ENDPOINT = "https://control.example.test/garam";

const READS = [
  "agent:create",
  "agent-template:read",
  "agent-template:publish",
  "execution-profile:read",
  "agent:execution-read",
];

/** One request the stub received, as it arrived. */
type Received = {
  method: string;
  url: URL;
  authorization: string | undefined;
  body: string;
};

type Answer = { status: number; body: unknown } | "unreachable";

function sha256(text: string): string {
  return createHash("sha256").update(text).digest("hex");
}

/** A published model's embeddings endpoint, as the wire carries it. */
const EMBEDDING = {
  baseUrl: "http://tei.embeddings.svc/v1",
  name: "BAAI/bge-base-en-v1.5",
  apiKeyRef: "",
};

/**
 * The control service's refusal of a publish whose model names no
 * embedding (garam-agent-operator ADR 0053 §Decision 1), the one case
 * of that rule these tests send. Null where a model names one.
 */
function refusedModel(body: string): Answer | null {
  const { model } = (
    JSON.parse(body) as {
      configuration: { model: { embedding?: unknown } };
    }
  ).configuration;
  if (model.embedding !== undefined) return null;
  return {
    status: 400,
    body: {
      kind: "embedding_required",
      message:
        "the model needs an embeddings endpoint with a base URL and a name",
    },
  };
}

/**
 * Answers every request to the registered endpoint with `answer`, under
 * the agreed CORS contract: the console's exact origin, the methods and
 * headers #220 lists, and no credentials.
 */
async function stubControl(
  page: Page,
  consoleOrigin: string,
  answer: (request: Received) => Answer,
): Promise<Received[]> {
  const received: Received[] = [];
  const cors = { "access-control-allow-origin": consoleOrigin };
  await page.route(`${new URL(ENDPOINT).origin}/**`, async (route) => {
    const request = route.request();
    if (request.method() === "OPTIONS") {
      await route.fulfill({
        status: 204,
        headers: {
          ...cors,
          "access-control-allow-methods": "GET, POST, PUT",
          "access-control-allow-headers":
            "Authorization, Content-Type, Garam-Contract-Version",
          "access-control-max-age": "600",
        },
      });
      return;
    }
    const got: Received = {
      method: request.method(),
      url: new URL(request.url()),
      authorization: (await request.allHeaders())["authorization"],
      body: request.postData() ?? "",
    };
    received.push(got);
    const reply = answer(got);
    if (reply === "unreachable") {
      await route.abort("connectionrefused");
      return;
    }
    await route.fulfill({
      status: reply.status,
      headers: cors,
      contentType: "application/json",
      body: JSON.stringify(reply.body),
    });
  });
  return received;
}

/**
 * Checks one received request against what garam recorded for the
 * authority it carried: the operation, the person, the audience, the
 * exact body and, for a read or publication, the exact target.
 */
async function expectBound(
  got: Received,
  seed: Seed,
  operation: string,
  target: string,
) {
  expect(got.authorization).toMatch(/^Garam-Operation [A-Za-z0-9_-]{43}$/);
  const authority = got.authorization!.slice("Garam-Operation ".length);
  expect(got.url.href).not.toContain(authority);
  const recorded = await recordedAuthority(authority);
  expect(recorded).not.toBeNull();
  expect(recorded!.operation).toBe(operation);
  expect(recorded!.actorGrn).toBe(seed.userGrn);
  expect(recorded!.audienceGrn).toBe(
    `grn:${seed.orgSlug}:default:operator:hosted`,
  );
  expect(recorded!.targetGrn).toBe(target);
  expect(recorded!.bodySha256).toBe(sha256(got.body));
  expect(recorded!.requestTarget).toBe(
    operation === "agent:create" ? null : got.url.pathname + got.url.search,
  );
  return recorded!;
}

test.describe("control service, as contract tests against the agreed Operator wire", () => {
  test("an administrator registers the endpoint and delegates apart from renaming the operator", async ({
    page,
    signedIn,
  }) => {
    await seedHostedOperator(signedIn, null, []);
    await page.goto(`/console/orgs/${signedIn.orgSlug}/operators/hosted`);
    await expect(
      page.getByText(
        "No control endpoint is registered. The console reaches no control service for this operator.",
        { exact: true },
      ),
    ).toBeVisible();

    const endpoint = page.getByLabel("Control endpoint", { exact: true });
    await expect(endpoint).toBeEnabled();
    await endpoint.fill(`${ENDPOINT}/`);
    await page
      .getByRole("button", { name: "Save endpoint", exact: true })
      .click();
    await expect(
      page.getByText(
        "control endpoint must be an https URL of at most 2048 bytes with a host and a valid port, and no credentials, query, fragment, dot segment or trailing slash",
        { exact: true },
      ),
    ).toBeVisible();
    await endpoint.fill(ENDPOINT);
    await page
      .getByRole("button", { name: "Save endpoint", exact: true })
      .click();
    await expect(page.getByText(ENDPOINT, { exact: true })).toBeVisible();

    await page.getByRole("checkbox", { name: /Field one/ }).check();
    for (const operation of ["agent-template:read", "agent:execution-read"]) {
      await page.getByRole("checkbox", { name: new RegExp(operation) }).check();
    }
    await page
      .getByRole("button", { name: "Grant delegation", exact: true })
      .click();
    await expect(
      page.getByText("Delegation granted.", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("agent-template:read, agent:execution-read", {
        exact: true,
      }),
    ).toBeVisible();

    const name = page.getByLabel("Name", { exact: true });
    await name.fill("Hosted renamed");
    await page
      .getByRole("button", { name: "Save details", exact: true })
      .click();
    await expect(
      page.getByText("Saved as Hosted renamed.", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(page.getByText(ENDPOINT, { exact: true })).toBeVisible();
  });

  test("lists, views and publishes templates, each call under its own bound authority", async ({
    page,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, READS);
    const base = `/garam/v1/orgs/${signedIn.orgSlug}`;
    let latest = 1;
    const received = await stubControl(
      page,
      new URL(baseURL!).origin,
      (got) => {
        const path = got.url.pathname;
        if (got.method === "GET" && path === `${base}/templates`) {
          return {
            status: 200,
            body: {
              templates: [
                {
                  name: "web agent",
                  version: latest,
                  profile: { name: "small", version: 1 },
                },
              ],
            },
          };
        }
        if (
          got.method === "GET" &&
          path === `${base}/templates/web%20agent/versions/1`
        ) {
          return {
            status: 200,
            body: {
              name: "web agent",
              version: 1,
              profile: { name: "small", version: 1 },
              configuration: {
                model: {
                  provider: "openai-compatible",
                  baseUrl: "https://api.example.test/v1",
                  name: "MiniMax-M2",
                  apiKeyRef: "model-keys/web",
                  embedding: EMBEDDING,
                },
                ego: "Answers web questions.",
                tools: { search: "v2" },
              },
            },
          };
        }
        if (got.method === "GET" && path === `${base}/profiles`) {
          return {
            status: 200,
            body: { profiles: [{ name: "small", version: 1 }] },
          };
        }
        if (
          got.method === "POST" &&
          path === `${base}/templates/web%20agent/versions`
        ) {
          const refused = refusedModel(got.body);
          if (refused !== null) return refused;
          latest = 2;
          return { status: 201, body: { name: "web agent", version: 2 } };
        }
        return { status: 404, body: { message: "no such route" } };
      },
    );

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/templates`);
    await expect(
      page.getByText("version 1, profile small version 1", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(
        /small version 1\. Profiles are published by the deployment/,
      ),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "View web agent", exact: true })
      .click();
    await expect(
      page.getByText("reference model-keys/web", { exact: true }),
    ).toBeVisible();

    await page
      .getByLabel("Ego", { exact: true })
      .fill("Answers web questions briefly.");
    await page
      .getByRole("button", { name: "Publish version", exact: true })
      .click();
    await expect(
      page.getByText(/^Published web agent version 2\./),
    ).toBeVisible();
    await expect(
      page.getByText("version 2, profile small version 1", { exact: true }),
    ).toBeVisible();

    const orgGrn = `grn:root:default:org:${signedIn.orgSlug}`;
    const list = received.find((r) => r.url.pathname === `${base}/templates`)!;
    await expectBound(list, signedIn, "agent-template:read", orgGrn);
    const version = received.find((r) =>
      r.url.pathname.endsWith("/versions/1"),
    )!;
    await expectBound(version, signedIn, "agent-template:read", orgGrn);
    const profiles = received.find(
      (r) => r.url.pathname === `${base}/profiles`,
    )!;
    await expectBound(profiles, signedIn, "execution-profile:read", orgGrn);
    const publish = received.find((r) => r.method === "POST")!;
    const recorded = await expectBound(
      publish,
      signedIn,
      "agent-template:publish",
      orgGrn,
    );
    const sent = JSON.parse(publish.body) as {
      requestId: string;
      configuration: { ego: string; model: unknown };
    };
    expect(sent.requestId).toBe(recorded.requestId);
    expect(sent.configuration.ego).toBe("Answers web questions briefly.");
    // The version on screen named an embedding, and publishing from it
    // sent the embedding unchanged.
    expect(sent.configuration.model).toEqual({
      provider: "openai-compatible",
      baseUrl: "https://api.example.test/v1",
      name: "MiniMax-M2",
      apiKeyRef: "model-keys/web",
      embedding: EMBEDDING,
    });
    // Each call minted its own authority: none was reused for another.
    const tokens = new Set(received.map((r) => r.authorization));
    expect(tokens.size).toBe(received.length);
  });

  test("publishes a model with the embedding the form names, which the control service refuses it without", async ({
    page,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, READS);
    const base = `/garam/v1/orgs/${signedIn.orgSlug}`;
    const received = await stubControl(
      page,
      new URL(baseURL!).origin,
      (got) => {
        const path = got.url.pathname;
        if (got.method === "GET" && path === `${base}/templates`) {
          return { status: 200, body: { templates: [] } };
        }
        if (got.method === "GET" && path === `${base}/profiles`) {
          return {
            status: 200,
            body: { profiles: [{ name: "small", version: 1 }] },
          };
        }
        if (
          got.method === "POST" &&
          path === `${base}/templates/minimax/versions`
        ) {
          return (
            refusedModel(got.body) ?? {
              status: 201,
              body: { name: "minimax", version: 1 },
            }
          );
        }
        return { status: 404, body: { message: "no such route" } };
      },
    );

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/templates`);
    await expect(
      page.getByText("No template is published.", { exact: true }),
    ).toBeVisible();
    const fields: [string, string][] = [
      ["Template", "minimax"],
      ["Profile", "small"],
      ["Profile version", "1"],
      ["Model provider", "openai-compatible"],
      ["Model base URL", "https://api.minimax.io/v1"],
      ["Model name", "MiniMax-M2"],
      ["API key reference", "minimax/key"],
    ];
    for (const [label, value] of fields) {
      await page.getByLabel(label, { exact: true }).fill(value);
    }
    const publish = page.getByRole("button", {
      name: "Publish version",
      exact: true,
    });

    // Named without an embedding, the model is refused.
    await publish.click();
    await expect(
      page.getByText(
        "the model needs an embeddings endpoint with a base URL and a name",
        { exact: true },
      ),
    ).toBeVisible();
    const refused = received.filter((r) => r.method === "POST");
    expect(refused).toHaveLength(1);
    expect(JSON.parse(refused[0]!.body).configuration.model).not.toHaveProperty(
      "embedding",
    );

    // The control: the same model, naming the embedding, is published.
    // Only the embedding is typed: the refusal left the rest as typed.
    await page
      .getByLabel("Embedding base URL", { exact: true })
      .fill(EMBEDDING.baseUrl);
    await page
      .getByLabel("Embedding model name", { exact: true })
      .fill(EMBEDDING.name);
    await publish.click();
    await expect(
      page.getByText(/^Published minimax version 1\./),
    ).toBeVisible();
    const accepted = received.filter((r) => r.method === "POST")[1]!;
    expect(JSON.parse(accepted.body).configuration.model.embedding).toEqual(
      EMBEDDING,
    );
  });

  test("creates an agent from a template and shows pending against runtime-reported execution", async ({
    page,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, READS);
    const agentGrn = `grn:${signedIn.orgSlug}:default:agent:${signedIn.agentIdentifier}`;
    const base = `/garam/v1/orgs/${signedIn.orgSlug}`;
    let reported = false;
    const received = await stubControl(
      page,
      new URL(baseURL!).origin,
      (got) => {
        const path = got.url.pathname;
        if (got.method === "GET" && path === `${base}/templates`) {
          return {
            status: 200,
            body: {
              templates: [
                {
                  name: "web agent",
                  version: 1,
                  profile: { name: "small", version: 1 },
                },
              ],
            },
          };
        }
        if (got.method === "POST" && path === `${base}/agents`) {
          return {
            status: 201,
            body: { agent: agentGrn, revision: "1", epoch: "1" },
          };
        }
        if (
          got.method === "GET" &&
          path === `${base}/agents/${encodeURIComponent(agentGrn)}/execution`
        ) {
          return {
            status: 200,
            body: {
              desired: { revision: "1", pending: !reported },
              rendered: reported ? { revision: "1" } : null,
              effective: reported
                ? {
                    revision: "1",
                    generation: "0123456789abcdef0123456789abcdef",
                    observedAt: "2026-10-05T03:00:00Z",
                  }
                : null,
            },
          };
        }
        return { status: 404, body: { message: "no such route" } };
      },
    );

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/new`);
    await expect(page.getByLabel("Template", { exact: true })).toBeVisible();
    await page
      .getByRole("button", { name: "Create from template", exact: true })
      .click();
    await expect(
      page.getByText(
        /at revision 1\. It keeps the template version it copied\./,
      ),
    ).toBeVisible();

    const create = received.find((r) => r.method === "POST")!;
    const controller = `grn:${signedIn.orgSlug}:default:operator:${signedIn.operatorIdentifier}`;
    const recorded = await expectBound(
      create,
      signedIn,
      "agent:create",
      controller,
    );
    expect(JSON.parse(create.body)).toEqual({
      requestId: recorded.requestId,
      controller,
      template: { name: "web agent", version: 1 },
      profile: { name: "small", version: 1 },
    });

    await page
      .getByRole("link", { name: signedIn.agentIdentifier, exact: true })
      .click();
    await expect(
      page.getByText("Revision 1 (pending)", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Not rendered yet", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("No runtime report", { exact: true }),
    ).toBeVisible();
    const execution = received.find((r) =>
      r.url.pathname.endsWith("/execution"),
    )!;
    await expectBound(execution, signedIn, "agent:execution-read", agentGrn);

    reported = true;
    await page.reload();
    await expect(page.getByText("Revision 1 (pending)")).toBeHidden();
    await expect(
      page.getByText("0123456789abcdef0123456789abcdef", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("No runtime report", { exact: true }),
    ).toBeHidden();
  });

  test("an unavailable control service disables only its own panels", async ({
    page,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, READS);
    await stubControl(page, new URL(baseURL!).origin, () => "unreachable");
    const unavailable = page.getByText(
      "The control service is unavailable, so its controls are disabled. Teams, people and agents are unaffected.",
      { exact: true },
    );

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/templates`);
    await expect(unavailable.first()).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Publish version" }),
    ).toBeHidden();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/new`);
    await expect(unavailable).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Create from template" }),
    ).toBeHidden();
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: "From the control service",
      }),
    ).toBeVisible();

    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/agents/${signedIn.agentIdentifier}`,
    );
    await expect(unavailable).toBeVisible();
    await expect(
      page.getByText(signedIn.agentProvisioningState, { exact: true }),
    ).toBeVisible();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/teams`);
    await expect(
      page.getByRole("link", { name: signedIn.groupName, exact: true }),
    ).toBeVisible();
    await page.goto(`/console/orgs/${signedIn.orgSlug}/people`);
    await expect(
      page.getByRole("heading", { level: 1, name: "People", exact: true }),
    ).toBeVisible();
    await expect(page.getByText(signedIn.nickname).first()).toBeVisible();
  });

  test("a member denied operator:read keeps the agent and template views, with the control service panels saying why", async ({
    page,
    context,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, READS);
    await stubControl(page, new URL(baseURL!).origin, () => "unreachable");
    const refused =
      "garam refused to list this organization's operators to you, so no control service is reachable from here.";
    const agentPage = `/console/orgs/${signedIn.orgSlug}/agents/${signedIn.agentIdentifier}`;

    // The control: the owner, whom nothing denies, meets no refusal on
    // the same page.
    await page.goto(agentPage);
    await expect(
      page.getByRole("heading", { level: 1, name: signedIn.agentIdentifier }),
    ).toBeVisible();
    await expect(page.getByText(refused, { exact: true })).toBeHidden();

    const member = await seedMemberDeniedOperatorRead(signedIn);
    await context.clearCookies();
    await context.addCookies([
      {
        name: "garam_session",
        value: member.sessionToken,
        url: baseURL!,
        httpOnly: true,
        sameSite: "Lax",
      },
    ]);

    await page.goto(agentPage);
    await expect(
      page.getByRole("heading", { level: 1, name: signedIn.agentIdentifier }),
    ).toBeVisible();
    await expect(
      page.getByText(signedIn.agentProvisioningState, { exact: true }),
    ).toBeVisible();
    await expect(page.getByText(refused, { exact: true })).toBeVisible();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/templates`);
    await expect(
      page.getByText("No archived templates", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText(refused, { exact: true })).toBeVisible();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/new`);
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: "From the control service",
      }),
    ).toBeVisible();
    await expect(page.getByText(refused, { exact: true })).toBeVisible();
  });

  test("a delegation read garam answers with 500 leaves the agent and template views, with the panels saying why", async ({
    page,
    signedIn,
    baseURL,
  }) => {
    await seedHostedOperator(signedIn, ENDPOINT, READS, "not-a-grn");
    await stubControl(page, new URL(baseURL!).origin, () => "unreachable");
    const failed = "garam did not answer for Hosted control's delegation.";

    // The control: the operator's own page reads the same delegation,
    // says so in its section, and renders around it.
    await page.goto(`/console/orgs/${signedIn.orgSlug}/operators/hosted`);
    await expect(
      page.getByRole("heading", { level: 1, name: "Hosted control" }),
    ).toBeVisible();
    await expect(
      page.getByText("garam did not answer for this operator's delegation.", {
        exact: true,
      }),
    ).toBeVisible();

    await page.goto(
      `/console/orgs/${signedIn.orgSlug}/agents/${signedIn.agentIdentifier}`,
    );
    await expect(
      page.getByRole("heading", { level: 1, name: signedIn.agentIdentifier }),
    ).toBeVisible();
    await expect(page.getByText(failed, { exact: true })).toBeVisible();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/templates`);
    await expect(
      page.getByText("No archived templates", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText(failed, { exact: true })).toBeVisible();

    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/new`);
    await expect(
      page.getByRole("heading", {
        level: 2,
        name: "From the control service",
      }),
    ).toBeVisible();
    await expect(page.getByText(failed, { exact: true })).toBeVisible();
  });

  test("with no registered endpoint the console reaches no control service and says so", async ({
    page,
    signedIn,
  }) => {
    const requests: string[] = [];
    page.on("request", (request) => {
      if (request.url().startsWith(new URL(ENDPOINT).origin)) {
        requests.push(request.url());
      }
    });
    await page.goto(`/console/orgs/${signedIn.orgSlug}/agents/templates`);
    await expect(
      page.getByText(
        "No control service is registered for this organization. An administrator registers one on an operator's page.",
        { exact: true },
      ),
    ).toBeVisible();
    expect(requests).toEqual([]);
  });
});
