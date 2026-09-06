import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DeleteOperatorForm } from "@/app/console/orgs/[org]/operators/_components/delete-operator-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// list it refreshes is what reports the record is gone.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Records the operators the form deleted, and answers as garam does. */
function recordDeletions() {
  const deleted: string[] = [];
  server.use(
    http.delete(`${API_URL}/orgs/acme/operators/:operator`, ({ request }) => {
      deleted.push(new URL(request.url).pathname);
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return deleted;
}

describe("delete operator form", () => {
  it("deletes on the confirmation and not on the click that asks for it", async () => {
    // The refusal and its control are read together: the first click
    // sends nothing, and the same recorder shows the confirmed one
    // sending — so what held the request back is the confirmation
    // rather than a form that never posts.
    const deleted = recordDeletions();
    const user = userEvent.setup();
    render(<DeleteOperatorForm org="acme" operator="field-one" enrolled />);

    await user.click(screen.getByRole("button", { name: "Delete" }));

    expect(deleted).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Confirm deletion" }));

    expect(deleted).toEqual(["/orgs/acme/operators/field-one"]);
  });

  it("sends nothing when the confirmation is cancelled", async () => {
    const deleted = recordDeletions();
    const user = userEvent.setup();
    render(<DeleteOperatorForm org="acme" operator="field-one" enrolled />);

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(deleted).toEqual([]);
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await user.click(screen.getByRole("button", { name: "Confirm deletion" }));

    expect(deleted).toEqual(["/orgs/acme/operators/field-one"]);
  });

  it("says the certificate is refused when the operator enrolled", async () => {
    const user = userEvent.setup();
    render(<DeleteOperatorForm org="acme" operator="field-one" enrolled />);

    await user.click(screen.getByRole("button", { name: "Delete" }));

    expect(
      screen.getByText(
        "field-one stops authenticating at once: the certificate it holds is refused from its next connection, and garam does not bring the record back. Agents it provisioned keep their own certificates.",
      ),
    ).toBeInTheDocument();
  });

  it("says nothing running stops when the operator never enrolled", async () => {
    const user = userEvent.setup();
    render(
      <DeleteOperatorForm org="acme" operator="field-one" enrolled={false} />,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));

    expect(
      screen.getByText(
        "field-one obtained no certificate, so nothing it runs stops. The record and its unspent enrollment token go, and the identifier is free to register again.",
      ),
    ).toBeInTheDocument();
  });

  it("renders the refusal the API answers with", async () => {
    server.use(
      http.delete(`${API_URL}/orgs/acme/operators/field-one`, () =>
        HttpResponse.json(
          {
            kind: "failed_precondition",
            message: "an agent definition names this operator",
          },
          { status: 409 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<DeleteOperatorForm org="acme" operator="field-one" enrolled />);

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await user.click(screen.getByRole("button", { name: "Confirm deletion" }));

    expect(
      await screen.findByText("an agent definition names this operator"),
    ).toBeInTheDocument();
  });
});
