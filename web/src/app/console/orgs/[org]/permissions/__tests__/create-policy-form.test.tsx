import { render, within } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { CreatePolicyForm } from "@/app/console/orgs/[org]/permissions/new/_components/create-policy-form";
import { enter } from "@/testing/enter";
import { API_URL, denyDeletes } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
const redirected: string[] = [];
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    redirected.push(url);
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

/**
 * Shows the error a render threw. In the app `redirect()` throws to
 * navigate and Next.js catches it; here this stands where Next.js
 * would, so the navigation is read rather than left unhandled.
 */
class Navigation extends Component<
  { children: ReactNode },
  { to: string | null }
> {
  state = { to: null };
  static getDerivedStateFromError(error: Error) {
    return { to: error.message };
  }
  render() {
    return this.state.to ?? this.props.children;
  }
}

const PolicyCreate = z.strictObject({
  name: z.string(),
  document: z.record(z.string(), z.unknown()),
});

describe("create policy form", () => {
  it("keeps the name and document as typed when garam refuses the document", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/policies`, async ({ request }) => {
        const { document } = PolicyCreate.parse(await request.json());
        return "Statement" in document
          ? HttpResponse.json(
              {
                kind: "invalid_argument",
                message: "policy document: Effect must be Allow or Deny",
              },
              { status: 400 },
            )
          : HttpResponse.json(denyDeletes, { status: 201 });
      }),
    );
    const user = userEvent.setup();
    const form = within(
      render(
        <Navigation>
          <CreatePolicyForm org="acme" />
        </Navigation>,
      ).container,
    );
    const name = form.getByLabelText("Policy name");
    const document = form.getByLabelText("Document");

    await enter(user, name, "Deny deletes");
    await user.clear(document);
    await enter(user, document, '{"Statement":[]}');
    await user.click(form.getByRole("button", { name: "Create draft" }));

    expect(await form.findByRole("alert")).toHaveTextContent(
      "policy document: Effect must be Allow or Deny",
    );
    expect(name).toHaveValue("Deny deletes");
    expect(document).toHaveValue('{"Statement":[]}');
    expect(redirected).toEqual([]);

    // The control: the document garam takes, sent from the same form,
    // opens the policy, so the refusal above was the document's.
    await user.clear(document);
    await enter(user, document, '{"Version":"2026-07-25"}');
    await user.click(form.getByRole("button", { name: "Create draft" }));
    expect(
      await form.findByText(
        "NEXT_REDIRECT:/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
      ),
    ).toBeInTheDocument();
  });

  it("holds back text that is not JSON and names why", async () => {
    const user = userEvent.setup();
    const form = within(render(<CreatePolicyForm org="acme" />).container);
    const submit = form.getByRole("button", { name: "Create draft" });
    // The control: the starting document parses, so the form can be
    // sent.
    expect(submit).toBeEnabled();

    await enter(user, form.getByLabelText("Document"), "oops");

    expect(form.getByRole("alert")).toHaveTextContent(
      "The document is not valid JSON.",
    );
    expect(submit).toBeDisabled();
  });
});
