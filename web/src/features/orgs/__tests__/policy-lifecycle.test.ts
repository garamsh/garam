import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { activatePolicy } from "@/features/orgs/api/activate-policy";
import { addPolicyVersion } from "@/features/orgs/api/add-policy-version";
import { archivePolicy } from "@/features/orgs/api/archive-policy";
import { createPolicy } from "@/features/orgs/api/create-policy";
import { rollbackPolicy } from "@/features/orgs/api/rollback-policy";
import {
  API_URL,
  baselineRead,
  denyDeletes,
  denyDeletesVersions,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const POLICIES = `${API_URL}/orgs/acme/policies`;
const DENY = `${POLICIES}/d1e2f3a4b5c6d7e8`;
const BASELINE = `${POLICIES}/0a1b2c3d4e5f6071`;

/** A body a write posts, parsed rather than asserted: no key beyond these. */
const PolicyCreate = z.strictObject({
  name: z.string(),
  document: z.record(z.string(), z.unknown()),
});
const PolicyVersionCreate = z.strictObject({
  document: z.record(z.string(), z.unknown()),
});
const PolicyRollback = z.strictObject({ version: z.number() });

const DOCUMENT = '{"Version":"2026-07-25","Statement":[]}';

function form(fields: Record<string, string>): FormData {
  const data = new FormData();
  data.set("org", "acme");
  for (const [key, value] of Object.entries(fields)) data.set(key, value);
  return data;
}

/** A 400 in the engine's words for a document it refuses. */
function refusedDocument() {
  return HttpResponse.json(
    {
      kind: "invalid_argument",
      message: "policy document: statement : Effect must be Allow or Deny",
    },
    { status: 400 },
  );
}

describe("createPolicy", () => {
  it("posts the name and the parsed document, then opens the draft", async () => {
    const posted: z.infer<typeof PolicyCreate>[] = [];
    server.use(
      http.post(POLICIES, async ({ request }) => {
        posted.push(PolicyCreate.parse(await request.json()));
        return HttpResponse.json(
          { ...denyDeletes, status: "draft" },
          { status: 201 },
        );
      }),
    );

    await expect(
      createPolicy(
        { status: "idle" },
        form({ name: " Deny project deletion ", document: DOCUMENT }),
      ),
    ).rejects.toThrow(
      "NEXT_REDIRECT:/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
    );
    expect(posted).toEqual([
      {
        name: "Deny project deletion",
        document: { Version: "2026-07-25", Statement: [] },
      },
    ]);
  });

  it("returns the engine's refusal of a document rather than opening anything", async () => {
    server.use(
      http.post(POLICIES, async ({ request }) => {
        const { document } = PolicyCreate.parse(await request.json());
        return "Bad" in document
          ? refusedDocument()
          : HttpResponse.json(denyDeletes, { status: 201 });
      }),
    );

    await expect(
      createPolicy(
        { status: "idle" },
        form({ name: "Broken", document: '{"Bad":1}' }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: "policy document: statement : Effect must be Allow or Deny",
    });

    // The control: the document the engine takes creates the policy, so
    // the refusal above is the document's and not every creation's.
    await expect(
      createPolicy(
        { status: "idle" },
        form({ name: "Fine", document: DOCUMENT }),
      ),
    ).rejects.toThrow("NEXT_REDIRECT:");
  });

  it("refuses text that is not a JSON object without asking garam", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(POLICIES, async ({ request }) => {
        posted.push(PolicyCreate.parse(await request.json()));
        return HttpResponse.json(denyDeletes, { status: 201 });
      }),
    );

    await expect(
      createPolicy({ status: "idle" }, form({ name: "X", document: "[1]" })),
    ).resolves.toEqual({
      status: "error",
      message: "The document must be a JSON object.",
    });
    expect(posted).toEqual([]);

    // The control: the same form carrying an object reaches garam, so
    // the refusal above is the array and not a form read as nothing.
    await expect(
      createPolicy({ status: "idle" }, form({ name: "X", document: DOCUMENT })),
    ).rejects.toThrow("NEXT_REDIRECT:");
    expect(posted).toHaveLength(1);
  });
});

describe("addPolicyVersion", () => {
  it("appends the document and answers with the version garam stored", async () => {
    const posted: z.infer<typeof PolicyVersionCreate>[] = [];
    server.use(
      http.post(`${DENY}/versions`, async ({ request }) => {
        posted.push(PolicyVersionCreate.parse(await request.json()));
        return HttpResponse.json(
          { ...denyDeletesVersions[1], version: 3 },
          { status: 201 },
        );
      }),
    );

    await expect(
      addPolicyVersion(
        { status: "idle" },
        form({ policy: "d1e2f3a4b5c6d7e8", document: DOCUMENT }),
      ),
    ).resolves.toEqual({ status: "saved", version: 3 });
    expect(posted).toEqual([
      { document: { Version: "2026-07-25", Statement: [] } },
    ]);
  });
});

describe("activatePolicy", () => {
  it("waits for the organization-wide confirmation on a policy with no attachment", async () => {
    const activated: string[] = [];
    server.use(
      http.post(`${BASELINE}/activate`, () => {
        activated.push("baseline");
        return HttpResponse.json(baselineRead);
      }),
    );

    await expect(
      activatePolicy({ status: "idle" }, form({ policy: "0a1b2c3d4e5f6071" })),
    ).resolves.toEqual({ status: "confirm-org-wide" });
    expect(activated).toEqual([]);

    // The control: the same form, confirmed, activates.
    await expect(
      activatePolicy(
        { status: "idle" },
        form({ policy: "0a1b2c3d4e5f6071", orgWide: "confirmed" }),
      ),
    ).resolves.toEqual({ status: "idle" });
    expect(activated).toEqual(["baseline"]);
  });

  it("activates an attached policy without that confirmation", async () => {
    const activated: string[] = [];
    server.use(
      http.post(`${DENY}/activate`, () => {
        activated.push("deny");
        return HttpResponse.json(denyDeletes);
      }),
    );

    await expect(
      activatePolicy({ status: "idle" }, form({ policy: "d1e2f3a4b5c6d7e8" })),
    ).resolves.toEqual({ status: "idle" });
    expect(activated).toEqual(["deny"]);
  });

  it.each([
    [
      "a known empty list asks to confirm everyone",
      () => HttpResponse.json([]),
      { status: "confirm-org-wide" },
    ],
    [
      "a refused list asks to confirm a scope it cannot know",
      () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      { status: "confirm-unknown-scope" },
    ],
    [
      "an unavailable list refuses to guess",
      () => HttpResponse.error(),
      { status: "error", message: "the garam API could not be reached" },
    ],
  ])("reads the scope before activating: %s", async (_, answer, state) => {
    const activated: string[] = [];
    server.use(
      http.post(`${DENY}/activate`, () => {
        activated.push("deny");
        return HttpResponse.json(denyDeletes);
      }),
    );
    // The control: with its attachment listed, the same activation
    // goes through, so each state below is the read's answer.
    await expect(
      activatePolicy({ status: "idle" }, form({ policy: "d1e2f3a4b5c6d7e8" })),
    ).resolves.toEqual({ status: "idle" });
    expect(activated).toEqual(["deny"]);

    server.use(http.get(`${DENY}/attachments`, answer));

    await expect(
      activatePolicy({ status: "idle" }, form({ policy: "d1e2f3a4b5c6d7e8" })),
    ).resolves.toEqual(state);
    expect(activated).toEqual(["deny"]);
  });
});

describe("rollbackPolicy", () => {
  it("posts the chosen version, newer or older than the default", async () => {
    const posted: z.infer<typeof PolicyRollback>[] = [];
    server.use(
      http.post(`${DENY}/rollback`, async ({ request }) => {
        posted.push(PolicyRollback.parse(await request.json()));
        return HttpResponse.json(denyDeletes);
      }),
    );

    for (const version of ["2", "1"]) {
      await expect(
        rollbackPolicy(
          { status: "idle" },
          form({ policy: "d1e2f3a4b5c6d7e8", version }),
        ),
      ).resolves.toEqual({ status: "idle" });
    }
    expect(posted).toEqual([{ version: 2 }, { version: 1 }]);
  });

  it("waits for the confirmation on an active policy with no attachment", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(`${BASELINE}/rollback`, async ({ request }) => {
        posted.push(PolicyRollback.parse(await request.json()));
        return HttpResponse.json(baselineRead);
      }),
    );

    await expect(
      rollbackPolicy(
        { status: "idle" },
        form({ policy: "0a1b2c3d4e5f6071", version: "1" }),
      ),
    ).resolves.toEqual({ status: "confirm-org-wide" });
    expect(posted).toEqual([]);

    // The control: the same policy as a draft evaluates nothing, so the
    // change needs no confirmation.
    server.use(
      http.get(POLICIES, () =>
        HttpResponse.json([{ ...baselineRead, status: "draft" }]),
      ),
    );
    await expect(
      rollbackPolicy(
        { status: "idle" },
        form({ policy: "0a1b2c3d4e5f6071", version: "1" }),
      ),
    ).resolves.toEqual({ status: "idle" });
    expect(posted).toEqual([{ version: 1 }]);
  });
});

describe("archivePolicy", () => {
  it("renders a refusal in garam's words and archives once allowed", async () => {
    let refusing = true;
    server.use(
      http.post(`${DENY}/archive`, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : HttpResponse.json({ ...denyDeletes, status: "archived" }),
      ),
    );

    await expect(
      archivePolicy({ status: "idle" }, form({ policy: "d1e2f3a4b5c6d7e8" })),
    ).resolves.toEqual({ status: "error", message: "permission denied" });

    // The control: the same archive, allowed, succeeds.
    refusing = false;
    await expect(
      archivePolicy({ status: "idle" }, form({ policy: "d1e2f3a4b5c6d7e8" })),
    ).resolves.toEqual({ status: "idle" });
  });
});
