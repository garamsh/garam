import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { mintOperatorEnrollment } from "@/features/orgs/api/mint-operator-enrollment";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const { revalidated } = vi.hoisted(() => ({ revalidated: [] as string[] }));
// `revalidatePath` only works inside a Next.js request store; the
// paths it was handed are what the page re-reads.
vi.mock("next/cache", () => ({
  revalidatePath: (path: string) => {
    revalidated.push(path);
  },
}));

const ENROLLMENT = `${API_URL}/orgs/acme/operators/:operator/enrollment`;
const SECTION = "/console/orgs/acme/operators";

const MINTED = {
  token: "c9f0f895fb98ab9159f51fd0297e236d",
  expiresAt: "2026-09-28T10:10:00Z",
};

const ALREADY_ENROLLED = {
  kind: "failed_precondition",
  message:
    "this operator has already enrolled: a second token would be a second credential path to an identity that has one, which garam does not decide to issue",
};

function form(operator = "field-two"): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("operator", operator);
  return data;
}

beforeEach(() => {
  revalidated.length = 0;
});

describe("mintOperatorEnrollment", () => {
  it("answers the new token and its expiry once, and re-reads the operators", async () => {
    const minted: string[] = [];
    server.use(
      http.post(ENROLLMENT, ({ params }) => {
        minted.push(String(params.operator));
        return HttpResponse.json(MINTED, { status: 201 });
      }),
    );

    await expect(
      mintOperatorEnrollment({ status: "idle" }, form()),
    ).resolves.toEqual({ status: "minted", ...MINTED });
    expect(minted).toEqual(["field-two"]);
    expect(revalidated).toEqual([SECTION]);
  });

  it("keeps an identifier that needs escaping as one path segment", async () => {
    const minted: string[] = [];
    server.use(
      http.post(ENROLLMENT, ({ params }) => {
        minted.push(String(params.operator));
        return HttpResponse.json(MINTED, { status: 201 });
      }),
    );

    await mintOperatorEnrollment({ status: "idle" }, form("a/b"));
    expect(minted).toEqual(["a/b"]);
  });

  it("answers an operator that already enrolled as that refusal, and re-reads the page that showed it without a certificate", async () => {
    let enrolled = true;
    server.use(
      http.post(ENROLLMENT, () =>
        enrolled
          ? HttpResponse.json(ALREADY_ENROLLED, { status: 409 })
          : HttpResponse.json(MINTED, { status: 201 }),
      ),
    );

    await expect(
      mintOperatorEnrollment({ status: "idle" }, form()),
    ).resolves.toEqual({ status: "already-enrolled" });
    expect(revalidated).toEqual([SECTION]);

    // The control: the same form minted for an operator without a
    // certificate answers a token, so the refusal above is garam's.
    enrolled = false;
    await expect(
      mintOperatorEnrollment({ status: "idle" }, form()),
    ).resolves.toEqual({ status: "minted", ...MINTED });
  });

  it("leaves the page as it was when garam refuses the caller", async () => {
    let refusing = true;
    server.use(
      http.post(ENROLLMENT, () =>
        refusing
          ? HttpResponse.json(
              { kind: "permission_denied", message: "permission denied" },
              { status: 403 },
            )
          : HttpResponse.json(MINTED, { status: 201 }),
      ),
    );

    await expect(
      mintOperatorEnrollment({ status: "idle" }, form()),
    ).resolves.toEqual({ status: "error", message: "permission denied" });
    expect(revalidated).toEqual([]);

    // The control: the same form accepted re-reads the page, so the
    // page left alone above is the refusal's doing.
    refusing = false;
    await mintOperatorEnrollment({ status: "idle" }, form());
    expect(revalidated).toEqual([SECTION]);
  });

  it("returns an unreachable API as a state the control renders", async () => {
    let reachable = false;
    server.use(
      http.post(ENROLLMENT, () =>
        reachable
          ? HttpResponse.json(MINTED, { status: 201 })
          : HttpResponse.error(),
      ),
    );

    await expect(
      mintOperatorEnrollment({ status: "idle" }, form()),
    ).resolves.toEqual({
      status: "error",
      message: "the garam API could not be reached",
    });
    expect(revalidated).toEqual([]);

    // The control: the same form against an API that answers mints.
    reachable = true;
    await expect(
      mintOperatorEnrollment({ status: "idle" }, form()),
    ).resolves.toEqual({ status: "minted", ...MINTED });
  });

  it("mints nothing for a form that names no operator", async () => {
    const minted: string[] = [];
    server.use(
      http.post(ENROLLMENT, ({ params }) => {
        minted.push(String(params.operator));
        return HttpResponse.json(MINTED, { status: 201 });
      }),
    );

    await expect(
      mintOperatorEnrollment({ status: "idle" }, form("")),
    ).resolves.toEqual({
      status: "error",
      message: "The form did not arrive complete.",
    });
    expect(minted).toEqual([]);

    // The control: the same recorder sees a form that names one.
    await mintOperatorEnrollment({ status: "idle" }, form());
    expect(minted).toEqual(["field-two"]);
  });
});
