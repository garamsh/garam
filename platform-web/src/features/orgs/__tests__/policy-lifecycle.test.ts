import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { activatePolicy } from "@/features/orgs/api/activate-policy";
import { deletePolicy } from "@/features/orgs/api/delete-policy";
import { rollbackPolicy } from "@/features/orgs/api/rollback-policy";
import {
  API_URL,
  DENY_DELETES_ID,
  ENGINEERING_ADMINS_ID,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import {
  REFUSAL_COPY,
  unmappedRefusalCopy,
} from "@garam/platform-sdk-ts/refusal-copy";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const POLICIES = `${API_URL}/orgs/acme/iam-policies`;
/** The policy every refusal below is answered for. */
const REFUSED = `${POLICIES}/${DENY_DELETES_ID}`;
/** The policy the same write is accepted for: the control. */
const ACCEPTED = `${POLICIES}/${ENGINEERING_ADMINS_ID}`;

/** The body a rollback posts, parsed rather than asserted. */
const PolicyRollback = z.strictObject({ version: z.number() });

/**
 * What each refusal the lifecycle can meet reads as. 403 and 503 have
 * sentences of their own; 409 and 504 are quoted as garam answers them.
 */
const REFUSALS = [
  {
    name: "the writer refusing it",
    status: 403,
    kind: "permission_denied",
    message: "iam:policy.update is not held",
    copy: REFUSAL_COPY.permissionDenied,
  },
  {
    name: "the default having moved",
    status: 409,
    kind: "failed_precondition",
    message: "the default version moved",
    copy: unmappedRefusalCopy("the default version moved"),
  },
  {
    name: "the policy service being unavailable",
    status: 503,
    kind: "unavailable",
    message: "the policy service is unreachable",
    copy: REFUSAL_COPY.unreachable,
  },
  {
    name: "the policy service timing out",
    status: 504,
    kind: "deadline_exceeded",
    message: "the policy service did not answer in time",
    copy: unmappedRefusalCopy("the policy service did not answer in time"),
  },
] as const;

function form(fields: Record<string, string>): FormData {
  const data = new FormData();
  data.set("org", "acme");
  for (const [key, value] of Object.entries(fields)) data.set(key, value);
  return data;
}

function refuse(refusal: (typeof REFUSALS)[number]) {
  return HttpResponse.json(
    { kind: refusal.kind, message: refusal.message },
    { status: refusal.status },
  );
}

describe("activatePolicy", () => {
  it("posts the activation to the policy's relay path", async () => {
    const posted: string[] = [];
    server.use(
      http.post(`${REFUSED}/activate`, () => {
        posted.push("activate");
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      activatePolicy({ status: "idle" }, form({ policy: DENY_DELETES_ID })),
    ).resolves.toEqual({ status: "idle" });
    expect(posted).toEqual(["activate"]);
  });

  it.each(REFUSALS)(
    "returns $name rather than reporting success",
    async (refusal) => {
      server.use(
        http.post(`${REFUSED}/activate`, () => refuse(refusal)),
        http.post(
          `${ACCEPTED}/activate`,
          () => new HttpResponse(null, { status: 204 }),
        ),
      );

      await expect(
        activatePolicy({ status: "idle" }, form({ policy: DENY_DELETES_ID })),
      ).resolves.toEqual({ status: "error", message: refusal.copy });

      // The control: the same write for a policy the service accepts is
      // a success, so the answer above is the refusal's and not every
      // activation's.
      await expect(
        activatePolicy(
          { status: "idle" },
          form({ policy: ENGINEERING_ADMINS_ID }),
        ),
      ).resolves.toEqual({ status: "idle" });
    },
  );

  it("refuses a form that names no policy without asking garam", async () => {
    const posted: string[] = [];
    server.use(
      http.post(`${POLICIES}/:policy/activate`, () => {
        posted.push("activate");
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(activatePolicy({ status: "idle" }, form({}))).resolves.toEqual(
      { status: "error", message: "The form did not arrive complete." },
    );
    expect(posted).toEqual([]);
  });
});

describe("rollbackPolicy", () => {
  it("posts the chosen version to the policy's relay path", async () => {
    const posted: z.infer<typeof PolicyRollback>[] = [];
    server.use(
      http.post(`${REFUSED}/rollback`, async ({ request }) => {
        posted.push(PolicyRollback.parse(await request.json()));
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      rollbackPolicy(
        { status: "idle" },
        form({ policy: DENY_DELETES_ID, version: "2" }),
      ),
    ).resolves.toEqual({ status: "idle" });
    expect(posted).toEqual([{ version: 2 }]);
  });

  it.each(REFUSALS)(
    "returns $name rather than reporting success",
    async (refusal) => {
      server.use(
        http.post(`${REFUSED}/rollback`, () => refuse(refusal)),
        http.post(
          `${ACCEPTED}/rollback`,
          () => new HttpResponse(null, { status: 204 }),
        ),
      );

      await expect(
        rollbackPolicy(
          { status: "idle" },
          form({ policy: DENY_DELETES_ID, version: "2" }),
        ),
      ).resolves.toEqual({ status: "error", message: refusal.copy });

      // The control: the same version of a policy the service accepts is
      // a success.
      await expect(
        rollbackPolicy(
          { status: "idle" },
          form({ policy: ENGINEERING_ADMINS_ID, version: "2" }),
        ),
      ).resolves.toEqual({ status: "idle" });
    },
  );

  it("refuses a version that is not a number without asking garam", async () => {
    const posted: string[] = [];
    server.use(
      http.post(`${REFUSED}/rollback`, () => {
        posted.push("rollback");
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      rollbackPolicy(
        { status: "idle" },
        form({ policy: DENY_DELETES_ID, version: "latest" }),
      ),
    ).resolves.toEqual({
      status: "error",
      message: "The form did not arrive complete.",
    });
    expect(posted).toEqual([]);
  });
});

describe("deletePolicy", () => {
  it("deletes the policy at its relay path, then opens the list", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(REFUSED, () => {
        deleted.push("deleted");
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await expect(
      deletePolicy({ status: "idle" }, form({ policy: DENY_DELETES_ID })),
    ).rejects.toThrow("NEXT_REDIRECT:/console/orgs/acme/permissions");
    expect(deleted).toEqual(["deleted"]);
  });

  it.each(REFUSALS)(
    "returns $name rather than opening the list",
    async (refusal) => {
      server.use(
        http.delete(REFUSED, () => refuse(refusal)),
        http.delete(ACCEPTED, () => new HttpResponse(null, { status: 204 })),
      );

      await expect(
        deletePolicy({ status: "idle" }, form({ policy: DENY_DELETES_ID })),
      ).resolves.toEqual({ status: "error", message: refusal.copy });

      // The control: a policy the service deletes opens the list.
      await expect(
        deletePolicy(
          { status: "idle" },
          form({ policy: ENGINEERING_ADMINS_ID }),
        ),
      ).rejects.toThrow("NEXT_REDIRECT:");
    },
  );
});
