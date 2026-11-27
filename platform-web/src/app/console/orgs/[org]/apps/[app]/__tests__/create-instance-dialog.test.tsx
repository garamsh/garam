import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreateInstanceDialog } from "@/app/console/orgs/[org]/apps/[app]/_components/create-instance-dialog";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { enter } from "@/testing/enter";
import { API_URL, fieldBot } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
}));

const INSTANCES = `${API_URL}/orgs/acme/installations/garam-bot-connector/instances`;

function answerCreate() {
  const bodies: unknown[] = [];
  server.use(
    http.post(INSTANCES, async ({ request }) => {
      bodies.push(await request.json());
      return HttpResponse.json(
        {
          instance: fieldBot,
          registration: {
            registrationToken: "reg-token-once",
            expiresAt: "2026-11-03T09:00:00Z",
          },
        },
        { status: 201 },
      );
    }),
  );
  return bodies;
}

async function open() {
  const user = userEvent.setup();
  render(
    <CreateInstanceDialog org="acme" installation="garam-bot-connector" />,
    { wrapper: TooltipProvider },
  );
  await user.click(
    await screen.findByRole("button", { name: "Create an instance" }),
  );
  return { user, dialog: within(screen.getByRole("dialog")) };
}

describe("create instance dialog", () => {
  it("sends what was typed, the kind as typed, and shows the token once", async () => {
    const bodies = answerCreate();
    const { user, dialog } = await open();

    await enter(user, dialog.getByLabelText("Name"), "Field bot");
    await enter(user, dialog.getByLabelText(/^Kind/), "Any Text ✓");
    await user.click(dialog.getByRole("button", { name: "Create instance" }));

    const reveal = await screen.findByRole("alertdialog", {
      name: "Registration token",
    });
    expect(reveal).toHaveTextContent("reg-token-once");
    expect(bodies).toEqual([{ name: "Field bot", kind: "Any Text ✓" }]);

    // Kept: the dialog closes, and the token is not on the page again.
    await user.click(within(reveal).getByRole("checkbox"));
    await user.click(within(reveal).getByRole("button", { name: "Done" }));
    expect(screen.queryByText("reg-token-once")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("cannot be dismissed with Done before the token is kept", async () => {
    answerCreate();
    const { user, dialog } = await open();

    await user.click(dialog.getByRole("button", { name: "Create instance" }));

    const reveal = await screen.findByRole("alertdialog", {
      name: "Registration token",
    });
    expect(within(reveal).getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("keeps garam's refusal in the dialog", async () => {
    server.use(
      http.post(INSTANCES, () =>
        HttpResponse.json(
          {
            kind: "failed_precondition",
            message: "an instance already exists",
          },
          { status: 409 },
        ),
      ),
    );
    const { user, dialog } = await open();

    await user.click(dialog.getByRole("button", { name: "Create instance" }));

    expect(await dialog.findByText(/already exists/)).toBeInTheDocument();
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });
});
