import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Component, type ReactNode } from "react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { UninstallForm } from "@/app/console/orgs/[org]/apps/[app]/_components/uninstall-form";
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

/** Shows where a successful uninstall's `redirect()` lands. */
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

function recordUninstalls() {
  const removed: string[] = [];
  server.use(
    http.delete(
      `${API_URL}/orgs/acme/installations/:installation`,
      ({ request }) => {
        removed.push(new URL(request.url).pathname);
        return new HttpResponse(null, { status: 204 });
      },
    ),
  );
  return removed;
}

function renderForm() {
  return render(
    <Navigation>
      <UninstallForm
        org="acme"
        installation="garam-bot-connector"
        name="Bot connector"
      />
    </Navigation>,
  );
}

describe("uninstall form", () => {
  it("says what goes with the installation before asking", async () => {
    const user = userEvent.setup();
    renderForm();

    await user.click(
      await screen.findByRole("button", { name: "Uninstall app" }),
    );

    const dialog = screen.getByRole("alertdialog", {
      name: "Uninstall Bot connector?",
    });
    expect(dialog).toHaveTextContent("its instances with their keys");
    expect(dialog).toHaveTextContent(
      "every policy that names the installation",
    );
    expect(dialog).toHaveTextContent("starts from none of them");
  });

  it("uninstalls once the slug is typed, and returns to the list", async () => {
    const removed = recordUninstalls();
    const user = userEvent.setup();
    renderForm();

    await user.click(
      await screen.findByRole("button", { name: "Uninstall app" }),
    );
    const dialog = within(screen.getByRole("alertdialog"));
    const confirm = dialog.getByRole("button", { name: "Uninstall app" });
    expect(confirm).toBeDisabled();
    await enter(
      user,
      dialog.getByLabelText(/Type garam-bot-connector to confirm/),
      "garam-bot-connector",
    );
    await user.click(confirm);

    expect(
      await screen.findByText("NEXT_REDIRECT:/console/orgs/acme/apps"),
    ).toBeInTheDocument();
    expect(removed).toEqual(["/orgs/acme/installations/garam-bot-connector"]);
  });

  it("sends nothing when cancelled", async () => {
    const removed = recordUninstalls();
    const user = userEvent.setup();
    renderForm();

    await user.click(
      await screen.findByRole("button", { name: "Uninstall app" }),
    );
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(removed).toEqual([]);
  });
});
