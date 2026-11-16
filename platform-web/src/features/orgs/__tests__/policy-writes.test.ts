import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { createPolicy } from "@/features/orgs/api/create-policy";
import { createPolicyVersion } from "@/features/orgs/api/create-policy-version";
import {
  API_URL,
  CREATED_POLICY_ID,
  DENY_DELETES_ID,
  ENGINEERING_ADMINS_ID,
  engineering,
  alice,
  aliceMembership,
  frontend,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { roleLinkChoices } from "@/features/orgs/role-link-choices";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const POLICIES = `${API_URL}/orgs/acme/iam-policies`;
const ORG = "grn:root/iam/org/acme";
const CEDAR = "forbid (principal, action, resource);";

function form(fields: Record<string, string>): FormData {
  const data = new FormData();
  for (const [key, value] of Object.entries(fields)) data.set(key, value);
  return data;
}

const IDLE = { status: "idle" } as const;

describe("createPolicy", () => {
  it("posts a guardrail's text and opens the draft the service stored", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(POLICIES, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(
          {
            id: CREATED_POLICY_ID,
            name: "Deny",
            kind: "guardrail",
            status: "draft",
            defaultVersion: 1,
          },
          { status: 201 },
        );
      }),
    );

    await expect(
      createPolicy(
        IDLE,
        form({ org: "acme", name: " Deny ", kind: "guardrail", text: CEDAR }),
      ),
    ).rejects.toThrow(
      `NEXT_REDIRECT:/console/orgs/acme/permissions/${CREATED_POLICY_ID}`,
    );
    expect(posted).toEqual([
      { name: "Deny", body: { kind: "guardrail", text: CEDAR } },
    ]);
  });

  it("posts a role link with a team principal over the organization", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(POLICIES, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(
          {
            id: CREATED_POLICY_ID,
            name: "Eng",
            kind: "role-link",
            status: "draft",
            defaultVersion: 1,
          },
          { status: 201 },
        );
      }),
    );

    await expect(
      createPolicy(
        IDLE,
        form({
          org: "acme",
          name: "Eng",
          kind: "role-link",
          role: "admin",
          scope: "organization",
          principal: engineering.grn,
          resource: ORG,
        }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT");
    expect(posted).toEqual([
      {
        name: "Eng",
        body: {
          kind: "role-link",
          roleLink: {
            role: "admin",
            scope: "organization",
            principal: engineering.grn,
            resource: ORG,
          },
        },
      },
    ]);
  });

  it("names a missing name at the name and asks the service nothing", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(POLICIES, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json({ id: CREATED_POLICY_ID }, { status: 201 });
      }),
    );

    await expect(
      createPolicy(
        IDLE,
        form({ org: "acme", name: "  ", kind: "guardrail", text: CEDAR }),
      ),
    ).resolves.toEqual({
      status: "error",
      at: "name",
      message: "Name the policy.",
    });
    expect(posted).toEqual([]);

    // The control: the same form with a name reaches the service.
    await expect(
      createPolicy(
        IDLE,
        form({ org: "acme", name: "Deny", kind: "guardrail", text: CEDAR }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT");
    expect(posted).toHaveLength(1);
  });

  it.each([
    ["a blank guardrail", { kind: "guardrail", text: "  " }],
    ["a permit with no text", { kind: "permit" }],
    ["a link with no role", { kind: "role-link", scope: "organization" }],
  ])("refuses %s at the body without asking the service", async (_, body) => {
    const posted: unknown[] = [];
    server.use(
      http.post(POLICIES, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json({ id: CREATED_POLICY_ID }, { status: 201 });
      }),
    );

    const state = await createPolicy(
      IDLE,
      form({ org: "acme", name: "N", ...body }),
    );

    expect(state).toMatchObject({ status: "error", at: "body" });
    expect(posted).toEqual([]);
  });

  it("shows the service's message at the body when it refuses the text", async () => {
    const refused = await createPolicy(
      IDLE,
      form({ org: "acme", name: "Bad", kind: "guardrail", text: "not cedar" }),
    );

    expect(refused).toEqual({
      status: "error",
      at: "body",
      message: "unexpected token at line 1: expected a policy",
    });
  });

  it("refuses a user principal at organization scope, which the platform derives", async () => {
    const link = {
      org: "acme",
      name: "Alice",
      kind: "role-link",
      role: "owner",
      scope: "organization",
      resource: ORG,
    };

    await expect(
      createPolicy(IDLE, form({ ...link, principal: alice.grn })),
    ).resolves.toMatchObject({
      status: "error",
      at: "body",
      message: "an organization role of a user is its membership's",
    });
    // The control: a team in the same place is stored.
    await expect(
      createPolicy(IDLE, form({ ...link, principal: engineering.grn })),
    ).rejects.toThrow("NEXT_REDIRECT");
  });

  it("refuses a link whose principal is its resource", async () => {
    const link = {
      org: "acme",
      name: "Self",
      kind: "role-link",
      role: "admin",
      scope: "team-or-project",
    };

    await expect(
      createPolicy(
        IDLE,
        form({
          ...link,
          principal: engineering.grn,
          resource: engineering.grn,
        }),
      ),
    ).resolves.toMatchObject({ status: "error", at: "body" });
    // The control: a person over the team is stored.
    await expect(
      createPolicy(
        IDLE,
        form({ ...link, principal: alice.grn, resource: engineering.grn }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT");
  });

  it.each([
    [403, "You can't create this kind of policy in this organization."],
    [409, "The policy service holds a policy that conflicts with this one."],
  ])(
    "says a %i at the form, worded for a Cedar policy",
    async (status, said) => {
      server.use(
        http.post(POLICIES, () =>
          HttpResponse.json({ kind: "refused", message: "x" }, { status }),
        ),
      );

      await expect(
        createPolicy(
          IDLE,
          form({ org: "acme", name: "N", kind: "permit", text: CEDAR }),
        ),
      ).resolves.toEqual({ status: "error", at: "form", message: said });
    },
  );

  it("says a role link's refusal in the words of a link", async () => {
    server.use(
      http.post(POLICIES, () =>
        HttpResponse.json({ kind: "denied", message: "x" }, { status: 403 }),
      ),
    );

    await expect(
      createPolicy(
        IDLE,
        form({
          org: "acme",
          name: "N",
          kind: "role-link",
          role: "admin",
          scope: "organization",
          principal: engineering.grn,
          resource: ORG,
        }),
      ),
    ).resolves.toMatchObject({
      at: "form",
      message: "You can't write a role link in this organization.",
    });
  });
});

describe("createPolicyVersion", () => {
  it("stores the text as the next version and says which", async () => {
    await expect(
      createPolicyVersion(
        IDLE,
        form({
          org: "acme",
          policy: DENY_DELETES_ID,
          kind: "guardrail",
          text: CEDAR,
        }),
      ),
    ).resolves.toEqual({ status: "stored", version: 3 });
  });

  it("shows the service's message at the body when it refuses the text", async () => {
    const fields = {
      org: "acme",
      policy: DENY_DELETES_ID,
      kind: "guardrail",
    };

    await expect(
      createPolicyVersion(IDLE, form({ ...fields, text: "not cedar" })),
    ).resolves.toEqual({
      status: "error",
      at: "body",
      message: "unexpected token at line 1: expected a policy",
    });
    // The control: Cedar it parses is stored.
    await expect(
      createPolicyVersion(IDLE, form({ ...fields, text: CEDAR })),
    ).resolves.toMatchObject({ status: "stored" });
  });

  it("takes no version of a role link, asking the service nothing", async () => {
    const posted: string[] = [];
    server.use(
      http.post(`${POLICIES}/:policy/versions`, ({ params }) => {
        posted.push(String(params.policy));
        return HttpResponse.json({ version: 2 }, { status: 201 });
      }),
    );

    await expect(
      createPolicyVersion(
        IDLE,
        form({
          org: "acme",
          policy: ENGINEERING_ADMINS_ID,
          kind: "role-link",
          text: CEDAR,
        }),
      ),
    ).resolves.toMatchObject({ status: "error" });
    expect(posted).toEqual([]);

    // The control: a guardrail's reaches it.
    await createPolicyVersion(
      IDLE,
      form({
        org: "acme",
        policy: DENY_DELETES_ID,
        kind: "guardrail",
        text: CEDAR,
      }),
    );
    expect(posted).toEqual([DENY_DELETES_ID]);
  });

  it.each([
    [403, "You can't add a version to this policy."],
    [404, "This policy is no longer in the organization."],
    [
      409,
      "The policy changed since this page was read. Reload the page and try again.",
    ],
  ])("says a %i at the form", async (status, said) => {
    server.use(
      http.post(`${POLICIES}/:policy/versions`, () =>
        HttpResponse.json({ kind: "refused", message: "x" }, { status }),
      ),
    );

    await expect(
      createPolicyVersion(
        IDLE,
        form({
          org: "acme",
          policy: DENY_DELETES_ID,
          kind: "guardrail",
          text: CEDAR,
        }),
      ),
    ).resolves.toEqual({ status: "error", at: "form", message: said });
  });
});

describe("roleLinkChoices", () => {
  const sources = {
    teams: [engineering],
    agents: [reportedAgent],
    members: [aliceMembership],
    projects: [frontend],
  };

  it("offers a team or an agent over the organization, never a person", () => {
    const { organizationPrincipals } = roleLinkChoices(sources);

    expect(organizationPrincipals.map(({ group }) => group)).toEqual([
      "Teams",
      "Agents",
    ]);
    expect(organizationPrincipals.map(({ value }) => value)).not.toContain(
      alice.grn,
    );
  });

  it("offers a person over a team or a project", () => {
    const { scopedPrincipals, scopedResources } = roleLinkChoices(sources);

    expect(scopedPrincipals.map(({ value }) => value)).toEqual([alice.grn]);
    expect(scopedResources.map(({ value }) => value)).toEqual([
      engineering.grn,
      frontend.grn,
    ]);
  });

  it("offers nothing from a list that was not read", () => {
    expect(
      roleLinkChoices({
        teams: null,
        agents: null,
        members: null,
        projects: null,
      }),
    ).toEqual({
      organizationPrincipals: [],
      scopedPrincipals: [],
      scopedResources: [],
    });
  });
});
