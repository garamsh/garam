import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConsoleSidebar } from "@/app/console/_components/console-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { unauthenticated } from "@/testing/mocks/handlers";
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

    expect(container.textContent).not.toContain("grn:");
  });

  it("sends a rejected session to the login page", async () => {
    server.use(unauthenticated());

    await expect(renderSidebar()).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
