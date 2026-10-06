import { render, screen } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { CreatePolicyForm } from "@/app/console/orgs/[org]/permissions/new/_components/create-policy-form";
import { API_URL, denyDeletes } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { setupUser } from "@/testing/user";

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
    const user = setupUser();
    render(
      <Navigation>
        <CreatePolicyForm org="acme" />
      </Navigation>,
    );
    const name = screen.getByLabelText("Policy name");
    const document = screen.getByLabelText("Document");

    await user.type(name, "Deny deletes");
    await user.clear(document);
    // user-event reads a doubled brace or bracket as the literal one.
    await user.type(document, '{{"Statement":[[]}');
    await user.click(screen.getByRole("button", { name: "Create draft" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "policy document: Effect must be Allow or Deny",
    );
    expect(name).toHaveValue("Deny deletes");
    expect(document).toHaveValue('{"Statement":[]}');
    expect(redirected).toEqual([]);

    // The control: the document garam takes, sent from the same form,
    // opens the policy, so the refusal above was the document's.
    await user.clear(document);
    await user.type(document, '{{"Version":"2026-07-25"}');
    await user.click(screen.getByRole("button", { name: "Create draft" }));
    expect(
      await screen.findByText(
        "NEXT_REDIRECT:/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
      ),
    ).toBeInTheDocument();
  });

  it("holds back text that is not JSON and names why", async () => {
    const user = setupUser();
    render(<CreatePolicyForm org="acme" />);
    const submit = screen.getByRole("button", { name: "Create draft" });
    // The control: the starting document parses, so the form can be
    // sent.
    expect(submit).toBeEnabled();

    await user.type(screen.getByLabelText("Document"), "oops");

    expect(screen.getByRole("alert")).toHaveTextContent(
      "The document is not valid JSON.",
    );
    expect(submit).toBeDisabled();
  });
});
