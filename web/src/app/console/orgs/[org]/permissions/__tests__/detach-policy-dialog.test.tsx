import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DetachPolicyDialog } from "@/app/console/orgs/[org]/permissions/_components/detach-policy-dialog";
import { API_URL, engineering } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const DETACH = `${API_URL}/orgs/acme/policies/d1e2f3a4b5c6d7e8/attachments/${encodeURIComponent(engineering.grn)}`;

describe("detach policy dialog", () => {
  it("names the ways forward when garam keeps the last attachment", async () => {
    let last = true;
    const deleted: string[] = [];
    server.use(
      http.delete(DETACH, () => {
        if (last) {
          return HttpResponse.json(
            { kind: "failed_precondition", message: "last attachment" },
            { status: 409 },
          );
        }
        deleted.push("eng");
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    render(
      <DetachPolicyDialog
        org="acme"
        policy="d1e2f3a4b5c6d7e8"
        subject={engineering.grn}
        policyName="Deny project deletion"
        subjectLabel="Engineering"
        policyHref="/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8"
      />,
    );

    // Nothing is said before a write lands, though detach answers
    // success with the state it starts in.
    expect(screen.queryByText(/^Detached/)).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", {
        name: "Detach Deny project deletion from Engineering",
      }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Detach Deny project deletion?",
    });
    // The confirming button names the act, never a bare "Confirm".
    await user.click(
      within(dialog).getByRole("button", { name: "Detach policy" }),
    );

    const refusal = await within(dialog).findByRole("alert");
    expect(refusal).toHaveTextContent(
      "Not detached: this is its last attachment",
    );
    expect(refusal).toHaveTextContent(
      "Attach it to another team, person or agent first, or archive the policy.",
    );
    expect(
      within(dialog).getByRole("link", { name: "Open Deny project deletion" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
    );

    // The control: with another attachment left, the same detach goes
    // through and the dialog closes.
    last = false;
    await user.click(
      within(dialog).getByRole("button", { name: "Detach policy" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(deleted).toEqual(["eng"]);
    expect(
      await screen.findByText("Detached from Engineering."),
    ).toBeInTheDocument();
  });
});
