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
    expect(screen.getByRole("link", { name: "Teams" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/teams",
    );
    expect(screen.getByRole("link", { name: "Permissions" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions",
    );
  });

  it("marks the section the caller is on", () => {
    renderNav("/console/orgs/acme/people");

    expect(screen.getByRole("link", { name: "People" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "Teams" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("marks the section, not the page, while on one team's page", () => {
    renderNav("/console/orgs/acme/teams/eng");

    expect(screen.getByRole("link", { name: "Teams" })).toHaveAttribute(
      "aria-current",
      "location",
    );
  });

  it("renders nothing until an organization is selected", () => {
    renderNav("/console/orgs");

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
