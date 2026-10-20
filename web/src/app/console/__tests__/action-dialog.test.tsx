import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { ActionDialog } from "@/app/console/_components/action-dialog";
import { Button } from "@/components/ui/button";
import { UNSENT_COPY } from "@/lib/unsent-action";

type State = { status: "idle" } | { status: "error"; message: string };

function renderDialog(action: (state: State) => Promise<State>) {
  render(
    <ActionDialog<State>
      trigger={<Button>Revoke key</Button>}
      title="Revoke this key?"
      description={<p>It stops working.</p>}
      action={action}
      initialState={{ status: "idle" }}
      fields={{}}
      confirmLabel="Revoke key"
      pendingLabel="Revoking…"
      done={(state) => state.status === "idle"}
    />,
  );
}

async function confirm() {
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Revoke key" }));
  const dialog = screen.getByRole("alertdialog", { name: "Revoke this key?" });
  await user.click(within(dialog).getByRole("button", { name: "Revoke key" }));
  return dialog;
}

describe("action dialog", () => {
  it("stays open and says garam's answer did not arrive, claiming nothing either way (#1631)", async () => {
    renderDialog(() =>
      Promise.reject(
        new Error("An unexpected response was received from the server."),
      ),
    );

    const dialog = await confirm();

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      UNSENT_COPY,
    );
    expect(
      screen.getByRole("alertdialog", { name: "Revoke this key?" }),
    ).toBeVisible();
  });

  it("renders a refusal the action returned, as before", async () => {
    // The control for the case above.
    renderDialog(async () => ({
      status: "error",
      message: "You can't revoke this key.",
    }));

    const dialog = await confirm();

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "You can't revoke this key.",
    );
    expect(dialog).not.toHaveTextContent(UNSENT_COPY);
  });
});
