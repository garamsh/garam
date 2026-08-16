import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConsoleNav } from "@/app/console/_components/console-nav";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";

const route = vi.hoisted(() => ({ pathname: "/console/orgs" }));

vi.mock("next/navigation", () => ({ usePathname: () => route.pathname }));

function renderNav(pathname: string) {
  route.pathname = pathname;
  return render(
    <TooltipProvider>
      <SidebarProvider>
        <ConsoleNav />
      </SidebarProvider>
    </TooltipProvider>,
  );
}

describe("console navigation", () => {
  it("links every section of the selected organization", () => {
    renderNav("/console/orgs/acme");

    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute(
      "href",
      "/console/orgs/acme",
    );
    expect(screen.getByRole("link", { name: "Groups" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/groups",
    );
    expect(screen.getByRole("link", { name: "Policies" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/policies",
    );
  });

  it("marks the section the caller is on", () => {
    renderNav("/console/orgs/acme/members");

    expect(screen.getByRole("link", { name: "Members" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "Groups" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("renders nothing until an organization is selected", () => {
    renderNav("/console/orgs");

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
