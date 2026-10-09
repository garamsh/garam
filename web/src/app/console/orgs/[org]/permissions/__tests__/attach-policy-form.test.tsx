import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { AttachPolicyForm } from "@/app/console/orgs/[org]/permissions/_components/attach-policy-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

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
        fixed={{ name: "policy", value: POLICY }}
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
    await user.selectOptions(
      screen.getByLabelText("Team, person or agent"),
      "Alice · Person",
    );
    await user.click(screen.getByRole("button", { name: "Attach" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );
    expect(posted).toEqual([{ subjectGrn: ALICE }]);
    expect(screen.getByLabelText("Team, person or agent")).toHaveValue(ALICE);
  });
});
