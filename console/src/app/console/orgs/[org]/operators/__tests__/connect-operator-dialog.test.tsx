import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ConnectOperatorDialog } from "@/app/console/orgs/[org]/operators/_components/connect-operator-dialog";
import { REFUSAL_COPY } from "@garam/platform-client/refusal-copy";
import { enter } from "@/testing/enter";
import { API_URL, fieldOne } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", async (importOriginal) => ({
  // The real `unstable_rethrow`, so the dialog tells its own navigation
  // from a failed request as it does in the browser.
  ...(await importOriginal<typeof import("next/navigation")>()),
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// The real action, unless a test makes its request fail before garam
// answers: Next.js then rejects the call, as it did for a 503 on aurora
// (#1631).
const transport = vi.hoisted(() => ({ fails: false }));
vi.mock("@/features/orgs/actions", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/orgs/actions")>();
  return {
    ...actual,
    registerOperator: (...args: Parameters<typeof actual.registerOperator>) =>
      transport.fails
        ? Promise.reject(
            new Error("An unexpected response was received from the server."),
          )
        : actual.registerOperator(...args),
  };
});

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the dialog renders. A registration that
// lands redirects, which
// features/orgs/__tests__/register-operator.test.ts holds.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Records every registration the dialog posts, and answers as garam does. */
function recordRegistrations() {
  const posted: unknown[] = [];
  server.use(
    http.post(`${API_URL}/orgs/acme/operators`, async ({ request }) => {
      posted.push(await request.json());
      return HttpResponse.json(fieldOne, { status: 201 });
    }),
  );
  return posted;
}

const fieldOneIdentity = {
  grn: fieldOne.grn,
  identifier: "field-one",
  name: fieldOne.name,
};

async function openDialog(
  props: Partial<Parameters<typeof ConnectOperatorDialog>[0]> = {},
) {
  const user = userEvent.setup();
  render(<ConnectOperatorDialog org="acme" {...props} />);
  await user.click(screen.getByRole("button", { name: "Connect an operator" }));
  const dialog = within(
    screen.getByRole("dialog", { name: "Connect an operator" }),
  );
  return { user, dialog };
}

describe("connect operator dialog", () => {
  it("stays open and says the operator may or may not be registered when garam's answer does not arrive", async () => {
    const posted = recordRegistrations();
    transport.fails = true;
    try {
      const { user, dialog } = await openDialog();
      await enter(
        user,
        dialog.getByRole("textbox", { name: "Cluster manager identifier" }),
        "aurora",
      );
      await enter(
        user,
        dialog.getByRole("textbox", { name: "Control service identifier" }),
        "aurora-control",
      );

      await user.click(dialog.getByRole("button", { name: "Register" }));

      expect(await dialog.findByRole("alert")).toHaveTextContent(
        "garam didn't answer, so the operator may or may not have been registered. Check the operator list before trying again.",
      );
      expect(
        screen.getByRole("dialog", { name: "Connect an operator" }),
      ).toBeVisible();
      expect(
        dialog.getByRole("textbox", { name: "Cluster manager identifier" }),
      ).toHaveValue("aurora");
      expect(posted).toEqual([]);
    } finally {
      transport.fails = false;
    }
  });

  it("refuses an identifier outside the character rule beside its field, without a round trip", async () => {
    const posted = recordRegistrations();
    const { user, dialog } = await openDialog();
    const identifier = dialog.getByRole("textbox", {
      name: "Cluster manager identifier",
    });

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
      dialog.getByRole("textbox", { name: "Cluster manager identifier" }),
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
      dialog.getByRole("textbox", { name: "Cluster manager identifier" }),
      "field-one",
    );
    await enter(
      user,
      dialog.getByRole("textbox", { name: "Control service identifier" }),
      "field-one-control",
    );

    await user.click(dialog.getByRole("button", { name: "Register" }));

    expect(await dialog.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.alreadyExists,
    );
    expect(dialog.queryByText("operator exists")).not.toBeInTheDocument();
    expect(
      dialog.getByRole("textbox", { name: "Cluster manager identifier" }),
    ).toHaveValue("field-one");
  });

  it("registers a new Operator's cluster manager and control service in one registration (#1636)", async () => {
    const posted = recordRegistrations();
    const { user, dialog } = await openDialog();
    await enter(
      user,
      dialog.getByRole("textbox", { name: "Cluster manager identifier" }),
      "aurora",
    );
    await enter(
      user,
      dialog.getByRole("textbox", { name: "Control service identifier" }),
      "aurora-control",
    );

    await user.click(dialog.getByRole("button", { name: "Register" }));

    await vi.waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toEqual(
      expect.objectContaining({
        identifier: "aurora",
        controlService: { identifier: "aurora-control" },
      }),
    );
    expect(posted[0]).not.toHaveProperty("manager");
  });

  it("refuses a control service under the cluster manager's own identifier, without a round trip", async () => {
    const posted = recordRegistrations();
    const { user, dialog } = await openDialog();
    await enter(
      user,
      dialog.getByRole("textbox", { name: "Cluster manager identifier" }),
      "aurora",
    );
    const control = dialog.getByRole("textbox", {
      name: "Control service identifier",
    });

    await enter(user, control, "aurora");

    expect(control).toHaveAttribute("aria-invalid", "true");
    expect(dialog.getByRole("button", { name: "Register" })).toBeDisabled();
    expect(posted).toEqual([]);
    // Control: an identifier of its own clears the mark.
    await user.clear(control);
    await enter(user, control, "aurora-control");
    expect(control).not.toHaveAttribute("aria-invalid");
  });

  it("registers a control service onto the cluster manager chosen, for an Operator connected before the two were registered together", async () => {
    const posted = recordRegistrations();
    const { user, dialog } = await openDialog({ managers: [fieldOneIdentity] });
    const choice = dialog.getByRole("combobox", { name: "Register" });
    expect(choice).toHaveTextContent("A new Operator");

    await user.click(choice);
    await user.click(
      // Presence: the option's name goes on with the identifier.
      await screen.findByRole("option", {
        name: /^The control service of Field cluster one/,
      }),
    );
    // One identity is registered, so the form asks for one identifier.
    expect(
      dialog.queryByRole("textbox", { name: "Cluster manager identifier" }),
    ).toBeNull();
    await enter(
      user,
      dialog.getByRole("textbox", { name: "Control service identifier" }),
      "field-one-control",
    );
    await user.click(dialog.getByRole("button", { name: "Register" }));

    await vi.waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toEqual(
      expect.objectContaining({
        identifier: "field-one-control",
        manager: fieldOne.grn,
      }),
    );
    expect(posted[0]).not.toHaveProperty("controlService");
  });

  it("opens on the cluster manager the address chose", async () => {
    const { dialog } = await openDialog({
      managers: [fieldOneIdentity],
      defaultManager: fieldOne.grn,
    });
    expect(
      dialog.getByRole("combobox", { name: "Register" }),
    ).toHaveTextContent("The control service of Field cluster one");
  });

  it("asks nothing about an existing Operator where every cluster manager has its control service", async () => {
    const { dialog } = await openDialog();
    expect(dialog.queryByRole("combobox", { name: "Register" })).toBeNull();
    // Control: the dialog is open with its form.
    expect(
      dialog.getByRole("textbox", { name: "Control service identifier" }),
    ).toBeVisible();
  });
});
