import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import SessionsPage from "@/app/console/sessions/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/sessions",
}));

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

function renderSessions() {
  return renderServerComponent(SessionsPage(), { wrapper: TooltipProvider });
}

describe("sessions page", () => {
  it("lists browser and CLI sessions, and marks the one it was opened on", async () => {
    await renderSessions();

    const table = screen.getByRole("table", { name: "Sessions" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => row.textContent)).toEqual([
      expect.stringContaining("CLI on alice-laptop"),
      expect.stringContaining("Browser"),
    ]);
    expect(rows[1]).toHaveTextContent("This session");
    // The control: the CLI's row is not the current one.
    expect(rows[0]).not.toHaveTextContent("This session");
  });

  it("revokes a session by its identifier after a confirmation", async () => {
    const deleted: string[] = [];
    server.use(
      http.delete(`${API_URL}/me/sessions/:session`, ({ params }) => {
        deleted.push(String(params.session));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    await renderSessions();

    await user.click(
      screen.getByRole("button", { name: "Revoke CLI on alice-laptop" }),
    );
    const dialog = screen.getByRole("alertdialog", {
      name: "Revoke CLI on alice-laptop?",
    });
    expect(dialog).toHaveTextContent("It stops working from its next request.");
    expect(deleted).toEqual([]);
    await user.click(
      within(dialog).getByRole("button", { name: "Revoke session" }),
    );

    await vi.waitFor(() => expect(deleted).toEqual(["s-cli"]));
  });

  it("warns that revoking the current session signs this browser out", async () => {
    const user = userEvent.setup();
    await renderSessions();

    await user.click(
      screen.getByRole("button", { name: "Revoke Browser (this session)" }),
    );

    expect(
      screen.getByRole("alertdialog", { name: "Revoke Browser?" }),
    ).toHaveTextContent("This signs you out here.");
  });

  it("says garam did not answer rather than showing no sessions", async () => {
    server.use(http.get(`${API_URL}/me/sessions`, () => HttpResponse.error()));

    await renderSessions();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("No sessions")).toBeNull();
  });
});
