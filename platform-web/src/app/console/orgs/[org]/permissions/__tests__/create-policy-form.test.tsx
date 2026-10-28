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
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";

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
        return JSON.stringify(document).includes("nope:nope")
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
    const action = form.getByRole("textbox", { name: "Statement 1 action 1" });

    await enter(user, name, "Deny deletes");
    await user.clear(action);
    await enter(user, action, "nope:nope");
    await user.click(form.getByRole("button", { name: "Create draft" }));

    // The refusal is the document's, so it stands under the editor.
    const editor = form.getByRole("group", { name: "Document" });
    expect(await within(editor).findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.policyDocumentRefused,
    );
    expect(name).toHaveValue("Deny deletes");
    expect(action).toHaveValue("nope:nope");
    expect(redirected).toEqual([]);

    // Editing the text the refusal was about takes the refusal away.
    await user.clear(action);
    await enter(user, action, "project:delete");
    expect(within(editor).queryByRole("alert")).not.toBeInTheDocument();

    // The control: the document garam takes, sent from the same form,
    // opens the policy, so the refusal above was the document's.
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

    await user.click(form.getByRole("tab", { name: "JSON" }));
    // CodeMirror loads in the browser only, after the first render.
    const json = await form.findByRole("textbox", { name: "Document JSON" });
    await user.click(json);
    await user.keyboard("{Control>}a{/Control}{Backspace}");
    await user.paste("oops");

    expect(await form.findByRole("alert")).toHaveTextContent(
      "The document is not valid JSON.",
    );
    expect(submit).toBeDisabled();
  });

  it("marks the control at the place garam's refusal names", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/policies`, () =>
        HttpResponse.json(
          {
            kind: "invalid_argument",
            message: "policy document: statement 0: Action is required",
            reason: "policy_document_action_missing",
            path: "/Statement/0/Action",
          },
          { status: 400 },
        ),
      ),
    );
    const user = userEvent.setup();
    const form = within(render(<CreatePolicyForm org="acme" />).container);

    await enter(user, form.getByLabelText("Policy name"), "Deny deletes");
    await user.click(form.getByRole("button", { name: "Create draft" }));

    // The sentence is the reason's, keyed on the code, not the message.
    const editor = form.getByRole("group", { name: "Document" });
    expect(await within(editor).findByRole("alert")).toHaveTextContent(
      "Statement 1 needs at least one Action.",
    );
    expect(
      form.getByRole("textbox", { name: "Statement 1 action 1" }),
    ).toHaveAttribute("aria-invalid", "true");
    // The control: the Resource beside it is not the place named.
    expect(
      form.getByRole("textbox", { name: "Statement 1 resource 1" }),
    ).not.toHaveAttribute("aria-invalid");
  });
});
