import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ConsoleSidebar } from "@/app/console/_components/console-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { API_URL, unauthenticated } from "@/testing/mocks/handlers";
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

    expect(screen.getByRole("button", { name: /Acme/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Members" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/members",
    );
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
    server.use(unauthenticated());

    await expect(renderSidebar()).rejects.toThrow("NEXT_REDIRECT:/login");
  });

  it("keeps the shell, without the controls its reads feed, when the API is unreachable", async () => {
    server.use(
      http.get(`${API_URL}/me`, () => HttpResponse.error()),
      http.get(`${API_URL}/orgs`, () => HttpResponse.error()),
    );

    await renderSidebar();

    expect(screen.getByRole("link", { name: "Members" })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Switch organization/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Open account menu/ }),
    ).not.toBeInTheDocument();
  });
});
