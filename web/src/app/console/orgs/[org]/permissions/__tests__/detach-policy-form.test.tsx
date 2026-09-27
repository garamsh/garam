import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DetachPolicyForm } from "@/app/console/orgs/[org]/permissions/_components/detach-policy-form";
import { API_URL, engineering } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const DETACH = `${API_URL}/orgs/acme/policies/d1e2f3a4b5c6d7e8/attachments/${encodeURIComponent(engineering.grn)}`;

function renderDetach() {
  return render(
    <DetachPolicyForm
      org="acme"
      policy="d1e2f3a4b5c6d7e8"
      subject={engineering.grn}
      policyName="Deny project deletion"
      subjectLabel="Engineering"
      policyHref="/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8"
    />,
  );
}

async function detach(user: ReturnType<typeof userEvent.setup>) {
  await user.click(
    screen.getByRole("button", {
      name: "Detach Deny project deletion from Engineering",
    }),
  );
  await user.click(screen.getByRole("button", { name: "Confirm detach" }));
}

describe("detach policy form", () => {
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
    renderDetach();

    await detach(user);

    const refusal = await screen.findByRole("alert");
    expect(refusal).toHaveTextContent(
      "Not detached: this is the last attachment of an active policy.",
    );
    expect(refusal).toHaveTextContent(
      "Attach it to another team, person or agent first, or archive the policy.",
    );
    expect(
      screen.getByRole("link", { name: "Open Deny project deletion" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
    );

    // The control: with another attachment left, the same detach goes
    // through and the refusal clears.
    last = false;
    await user.click(screen.getByRole("button", { name: "Confirm detach" }));
    await vi.waitFor(() => expect(deleted).toEqual(["eng"]));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
