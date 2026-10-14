import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ConnectOperatorDialog } from "@/app/console/orgs/[org]/operators/_components/connect-operator-dialog";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
import { enter } from "@/testing/enter";
import { API_URL, fieldOneRegistered } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the dialog renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Records every registration the dialog posts, and answers as garam does. */
function recordRegistrations() {
  const posted: unknown[] = [];
  server.use(
    http.post(`${API_URL}/orgs/acme/operators`, async ({ request }) => {
      posted.push(await request.json());
      return HttpResponse.json(fieldOneRegistered, { status: 201 });
    }),
  );
  return posted;
}

async function openDialog() {
  const user = userEvent.setup();
  render(
    <ConnectOperatorDialog
      org="acme"
      operatorsHref="/console/orgs/acme/operators"
    />,
  );
  await user.click(screen.getByRole("button", { name: "Connect an operator" }));
  const dialog = within(
    screen.getByRole("dialog", { name: "Connect an operator" }),
  );
  return { user, dialog };
}

describe("connect operator dialog", () => {
  it("registers the identifier alone when no name is given, and continues to its setup", async () => {
    const posted = recordRegistrations();
    const { user, dialog } = await openDialog();

    await enter(
      user,
      dialog.getByRole("textbox", { name: "Identifier" }),
      "field-one",
    );
    await user.click(dialog.getByRole("button", { name: "Register" }));

    expect(
      await screen.findByRole("link", { name: /Continue: issue a key/ }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one/connect");
    // No name is sent, so the API calls the operator by its identifier
    // (ADR-0074).
    expect(posted).toEqual([{ identifier: "field-one", description: "" }]);
  });

  it("keeps the legacy secrets closed until asked, and reveals each once", async () => {
    recordRegistrations();
    const { user, dialog } = await openDialog();
    await enter(
      user,
      dialog.getByRole("textbox", { name: "Identifier" }),
      "field-one",
    );
    await user.click(dialog.getByRole("button", { name: "Register" }));
    const toggle = await screen.findByRole("button", {
      name: "Legacy credential and enrollment token",
    });

    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.queryByText(fieldOneRegistered.credential.credential),
    ).not.toBeInTheDocument();

    await user.click(toggle);
    await user.click(screen.getByRole("button", { name: "Show credential" }));

    expect(
      screen.getByRole("alertdialog", { name: "Save the legacy credential" }),
    ).toHaveTextContent(fieldOneRegistered.credential.credential);
  });

  it("refuses an identifier outside the character rule beside its field, without a round trip", async () => {
    const posted = recordRegistrations();
    const { user, dialog } = await openDialog();
    const identifier = dialog.getByRole("textbox", { name: "Identifier" });

    await enter(user, identifier, "Field One");

    expect(identifier).toHaveAttribute("aria-invalid", "true");
    expect(dialog.getByRole("button", { name: "Register" })).toBeDisabled();
    expect(posted).toEqual([]);
    // Control: a valid identifier clears the mark.
    await user.clear(identifier);
    await enter(user, identifier, "field-one");
    expect(identifier).not.toHaveAttribute("aria-invalid");
  });

  it("refuses a name past 100 characters beside the name", async () => {
    const { user, dialog } = await openDialog();
    const name = dialog.getByRole("textbox", { name: "Name" });

    await enter(user, name, "x".repeat(101));

    expect(name).toHaveAttribute("aria-invalid", "true");
    expect(
      dialog.getByRole("textbox", { name: "Identifier" }),
    ).not.toHaveAttribute("aria-invalid");
  });

  it("renders garam's refusal in human words and keeps what was typed", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "operator exists" },
          { status: 409 },
        ),
      ),
    );
    const { user, dialog } = await openDialog();
    await enter(
      user,
      dialog.getByRole("textbox", { name: "Identifier" }),
      "field-one",
    );

    await user.click(dialog.getByRole("button", { name: "Register" }));

    expect(await dialog.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.alreadyExists,
    );
    expect(dialog.queryByText("operator exists")).not.toBeInTheDocument();
    expect(dialog.getByRole("textbox", { name: "Identifier" })).toHaveValue(
      "field-one",
    );
  });
});
