import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

import { CreatePolicyForm } from "@/app/console/orgs/[org]/permissions/new/_components/create-policy-form";
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
    render(<CreatePolicyForm org="acme" />);
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
  });

  it("holds back text that is not JSON and names why", async () => {
    const user = userEvent.setup();
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
