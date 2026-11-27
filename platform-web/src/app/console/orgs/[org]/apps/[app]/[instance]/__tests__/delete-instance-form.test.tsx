import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Component, type ReactNode } from "react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DeleteInstanceForm } from "@/app/console/orgs/[org]/apps/[app]/[instance]/_components/delete-instance-form";
import { enter } from "@/testing/enter";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("next/navigation")>()),
  redirect: (url: string) => {
    throw Object.assign(new Error(`NEXT_REDIRECT:${url}`), {
      digest: `NEXT_REDIRECT;push;${url};307;`,
    });
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Shows where a successful delete's `redirect()` lands. */
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

describe("delete instance form", () => {
  it("deletes once the identifier is typed, and returns to the installation", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(
        `${API_URL}/orgs/acme/installations/garam-bot-connector/instances/:instance`,
        ({ request }) => {
          deleted.push(new URL(request.url).pathname);
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );
    const user = userEvent.setup();
    render(
      <Navigation>
        <DeleteInstanceForm
          org="acme"
          installation="garam-bot-connector"
          instance="i-1a2b3c"
          name="Field bot"
        />
      </Navigation>,
    );

    await user.click(
      await screen.findByRole("button", { name: "Delete instance" }),
    );
    const dialog = within(
      screen.getByRole("alertdialog", { name: "Delete Field bot?" }),
    );
    const confirm = dialog.getByRole("button", { name: "Delete instance" });
    expect(confirm).toBeDisabled();
    await enter(
      user,
      dialog.getByLabelText(/Type i-1a2b3c to confirm/),
      "i-1a2b3c",
    );
    await user.click(confirm);

    expect(
      await screen.findByText(
        "NEXT_REDIRECT:/console/orgs/acme/apps/garam-bot-connector",
      ),
    ).toBeInTheDocument();
    expect(deleted).toEqual([
      "/orgs/acme/installations/garam-bot-connector/instances/i-1a2b3c",
    ]);
  });
});
