import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ConsoleSidebar } from "@/app/console/_components/console-sidebar";
import { SidebarProvider } from "@garam/design/ui/sidebar";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { API_URL } from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { RETURN_PATH_HEADER } from "@garam/platform-sdk-ts/return-path";
import { requestHeaders, server } from "@/testing/setup";

const route = vi.hoisted(() => ({ pathname: "/console/orgs/acme" }));

// `redirect()` throws a control-flow error the router catches; the test
// only needs to observe where it was sent.
vi.mock("next/navigation", () => ({
  usePathname: () => route.pathname,
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <TooltipProvider>
      <SidebarProvider>{children}</SidebarProvider>
    </TooltipProvider>
  );
}

function renderSidebar() {
  return renderServerComponent(ConsoleSidebar(), { wrapper: Shell });
}

describe("console sidebar", () => {
  it("carries the switcher, the org's sections and the signed-in user", async () => {
    await renderSidebar();

    // Presence: the switcher's name is the sr-only "Switch
    // organization", the org's name and its slug.
    expect(screen.getByRole("button", { name: /Acme/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "People" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/people",
    );
    // Presence: the account menu's name is the sr-only "Open account
    // menu", the avatar's initials, the nickname and "Signed in".
    expect(screen.getByRole("button", { name: /Alice/ })).toBeInTheDocument();
  });

  it("carries no GRN", async () => {
    const { container } = await renderSidebar();

    // The control for the search: the sidebar rendered, so what is
    // absent is the GRN and not the whole tree.
    expect(screen.getByRole("link", { name: "People" })).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("sends a rejected session to the login page, carrying the page it was on", async () => {
    // Only `/me` refuses, so the redirect is the sidebar's own session
    // check and not the organization read's beside it.
    server.use(
      http.get(`${API_URL}/me`, () =>
        HttpResponse.json(
          { kind: "unauthenticated", message: "no session" },
          { status: 401 },
        ),
      ),
    );
    requestHeaders.set(RETURN_PATH_HEADER, "/console/invitations/tok-123");

    await expect(renderSidebar()).rejects.toThrow(
      /^NEXT_REDIRECT:\/auth\/sign-in\?next=%2Fconsole%2Finvitations%2Ftok-123$/,
    );
  });

  it("keeps every control, degraded, when the API is unreachable", async () => {
    const user = userEvent.setup();
    server.use(
      http.get(`${API_URL}/me`, () => HttpResponse.error()),
      http.get(`${API_URL}/orgs`, () => HttpResponse.error()),
    );

    await renderSidebar();

    expect(screen.getByRole("link", { name: "People" })).toBeInTheDocument();
    // Presence: the sr-only label opens the trigger's name, and the
    // slug and the reason follow it.
    const switcher = screen.getByRole("button", {
      name: /Switch organization/,
    });
    expect(switcher).toHaveTextContent("acme");
    expect(switcher).toHaveTextContent("Can’t load organizations");
    // Presence, as above: "Account" and the reason follow.
    const account = screen.getByRole("button", { name: /Open account menu/ });
    expect(account).toHaveTextContent("Can’t load your account");

    // What the account menu offers needs no answer from the API.
    await user.click(account);
    expect(
      await screen.findByRole("menuitemradio", { name: "Dark" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: "Sign out" }),
    ).toBeInTheDocument();
  });
});
