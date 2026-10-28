import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { AttachPolicyForm } from "@/app/console/orgs/[org]/permissions/_components/attach-policy-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const POLICY = "grn:acme:default:policy:deny-renames";
const ENGINEERING = "grn:acme:default:group:eng";
const ALICE = "grn:acme:default:user:alice";

describe("attach policy form", () => {
  it("keeps the subject chosen when garam refuses the attachment", async () => {
    const posted: unknown[] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/policies/${encodeURIComponent(POLICY)}/attachments`,
        async ({ request }) => {
          posted.push(await request.json());
          return HttpResponse.json(
            { kind: "permission_denied", message: "permission denied" },
            { status: 403 },
          );
        },
      ),
    );
    const user = userEvent.setup();
    render(
      <AttachPolicyForm
        org="acme"
        fixed={{ name: "policy", value: POLICY, label: "Deny renames" }}
        choose={{
          label: "Team, person or agent",
          placeholder: "Choose whom to attach it to",
          groups: [
            {
              label: "Teams",
              options: [{ value: ENGINEERING, label: "Engineering · Team" }],
            },
            {
              label: "People",
              options: [{ value: ALICE, label: "Alice · Person" }],
            },
          ],
        }}
        empty="Nothing to attach."
      />,
    );

    // Not the first offered, so a reset to it would show as well as a
    // reset to the prompt.
    const choice = screen.getByRole("combobox", {
      name: "Team, person or agent",
    });
    await user.click(choice);
    await user.click(
      await screen.findByRole("option", { name: "Alice · Person" }),
    );
    await user.click(screen.getByRole("button", { name: "Attach" }));

    // The refusal is the choice's, so it stands under it.
    expect(await screen.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(choice).toHaveAccessibleDescription(REFUSAL_COPY.permissionDenied);
    expect(posted).toEqual([{ subjectGrn: ALICE }]);
    expect(choice).toHaveTextContent("Alice · Person");
  });

  it("names the subject a chosen policy was attached to", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/policies/deny-renames/attachments`, () =>
        HttpResponse.json(
          {
            policyGrn: POLICY,
            subjectGrn: ALICE,
            createdAt: "2026-01-02T03:04:05Z",
          },
          { status: 201 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(
      <AttachPolicyForm
        org="acme"
        fixed={{ name: "subject", value: ALICE, label: "Alice" }}
        choose={{
          label: "Attach a policy",
          placeholder: "Choose a policy to attach",
          groups: [
            {
              label: "Policies",
              options: [
                {
                  value: "deny-renames",
                  label: "Deny renames · Draft",
                  name: "Deny renames",
                },
              ],
            },
          ],
        }}
        empty="Nothing to attach."
      />,
    );

    await user.click(screen.getByRole("combobox", { name: "Attach a policy" }));
    await user.click(
      await screen.findByRole("option", { name: "Deny renames · Draft" }),
    );
    await user.click(screen.getByRole("button", { name: "Attach" }));

    // The policy went to Alice: the sentence names her as the target.
    expect(
      await screen.findByText("Attached Deny renames to Alice."),
    ).toBeInTheDocument();
  });
});
