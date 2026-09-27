import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { attachPolicy } from "@/features/orgs/api/attach-policy";
import { detachPolicy } from "@/features/orgs/api/detach-policy";
import {
  API_URL,
  denyDeletes,
  engineering,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ATTACHMENTS = `${API_URL}/orgs/acme/policies/d1e2f3a4b5c6d7e8/attachments`;

/** The body an attachment posts, parsed rather than asserted. */
const PolicyAttachmentCreate = z.strictObject({ subjectGrn: z.string() });

function form(subject: string): FormData {
  const data = new FormData();
  data.set("org", "acme");
  data.set("policy", "d1e2f3a4b5c6d7e8");
  data.set("subject", subject);
  return data;
}

function attachment(subjectGrn: string) {
  return HttpResponse.json(
    {
      policyGrn: denyDeletes.grn,
      subjectGrn,
      createdAt: "2026-01-02T03:04:05Z",
    },
    { status: 201 },
  );
}

describe("attachPolicy", () => {
  it("posts the chosen subject's GRN", async () => {
    const posted: z.infer<typeof PolicyAttachmentCreate>[] = [];
    server.use(
      http.post(ATTACHMENTS, async ({ request }) => {
        const body = PolicyAttachmentCreate.parse(await request.json());
        posted.push(body);
        return attachment(body.subjectGrn);
      }),
    );

    await expect(
      attachPolicy({ status: "idle" }, form(reportedAgent.grn)),
    ).resolves.toEqual({ status: "idle" });
    expect(posted).toEqual([{ subjectGrn: reportedAgent.grn }]);
  });

  it("says an attachment made since the page was read is already there", async () => {
    server.use(
      http.post(ATTACHMENTS, async ({ request }) => {
        const { subjectGrn } = PolicyAttachmentCreate.parse(
          await request.json(),
        );
        return subjectGrn === engineering.grn
          ? HttpResponse.json(
              {
                kind: "already_exists",
                message: "policy attachment already exists",
              },
              { status: 409 },
            )
          : attachment(subjectGrn);
      }),
    );

    await expect(
      attachPolicy({ status: "idle" }, form(engineering.grn)),
    ).resolves.toEqual({
      status: "error",
      message: "Already attached. The page now shows it.",
    });

    // The control: a subject not attached yet is attached.
    await expect(
      attachPolicy({ status: "idle" }, form(reportedAgent.grn)),
    ).resolves.toEqual({ status: "idle" });
  });
});

describe("detachPolicy", () => {
  it("returns the last-attachment refusal as its own state", async () => {
    const deleted: string[] = [];
    // garam holds two attachments and keeps the last one.
    const held = new Set([engineering.grn, reportedAgent.grn]);
    server.use(
      http.delete(`${ATTACHMENTS}/:subject`, ({ params }) => {
        const subject = z.string().parse(params.subject);
        if (held.size === 1 && held.has(subject)) {
          return HttpResponse.json(
            {
              kind: "failed_precondition",
              message: "detaching an active policy's last attachment",
            },
            { status: 409 },
          );
        }
        held.delete(subject);
        deleted.push(subject);
        return new HttpResponse(null, { status: 204 });
      }),
    );

    // The control: with another attachment left, the same kind of
    // detach succeeds, so the refusal below is the last one's.
    await expect(
      detachPolicy({ status: "idle" }, form(reportedAgent.grn)),
    ).resolves.toEqual({ status: "idle" });
    expect(deleted).toEqual([reportedAgent.grn]);

    await expect(
      detachPolicy({ status: "idle" }, form(engineering.grn)),
    ).resolves.toEqual({ status: "last-attachment" });
    expect(deleted).toEqual([reportedAgent.grn]);
  });
});
