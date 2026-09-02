import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ConsoleSidebar } from "@/app/console/_components/console-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { API_URL, signedOut } from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

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
    expect(screen.getByRole("link", { name: "Members" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/members",
    );
    // Presence: the account menu's name is the sr-only "Open account
    // menu", the avatar's initials, the nickname and "Signed in".
    expect(screen.getByRole("button", { name: /Alice/ })).toBeInTheDocument();
  });

  it("carries no GRN", async () => {
    const { container } = await renderSidebar();

    // The control for the search: the sidebar rendered, so what is
    // absent is the GRN and not the whole tree.
    expect(screen.getByRole("link", { name: "Members" })).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("sends a rejected session to the login page", async () => {
    server.use(...signedOut());

    await expect(renderSidebar()).rejects.toThrow("NEXT_REDIRECT:/login");
  });

  it("keeps the shell, without the controls its reads feed, when the API is unreachable", async () => {
    server.use(
      http.get(`${API_URL}/me`, () => HttpResponse.error()),
      http.get(`${API_URL}/orgs`, () => HttpResponse.error()),
    );

    await renderSidebar();

    expect(screen.getByRole("link", { name: "Members" })).toBeInTheDocument();
    // Presence: the sr-only label opens a name the org's own name and
    // slug complete, so it stays a fragment when the trigger renders.
    expect(
      screen.queryByRole("button", { name: /Switch organization/ }),
    ).not.toBeInTheDocument();
    // Presence, as above: the initials, the nickname and "Signed in"
    // complete this one.
    expect(
      screen.queryByRole("button", { name: /Open account menu/ }),
    ).not.toBeInTheDocument();
  });
});
