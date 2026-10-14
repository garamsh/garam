import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConsoleNav } from "@/app/console/_components/console-nav";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";

const route = vi.hoisted(() => ({ pathname: "/console/orgs" }));

vi.mock("next/navigation", () => ({ usePathname: () => route.pathname }));

const ORGS = [{ slug: "acme", name: "Acme" }];

function renderNav(
  pathname: string,
  orgs: { slug: string; name: string }[] | null = ORGS,
) {
  route.pathname = pathname;
  return render(
    <TooltipProvider>
      <SidebarProvider>
        <ConsoleNav orgs={orgs} />
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

  it("groups the sections as Workloads, Access, Infrastructure and Organization", () => {
    renderNav("/console/orgs/acme");

    const groups = {
      Workloads: ["Agents", "Templates"],
      Access: ["Teams", "People", "Permissions"],
      Infrastructure: ["Operators"],
      Organization: ["Projects", "Consents"],
    };
    for (const [group, sections] of Object.entries(groups)) {
      const list = screen.getByRole("list", { name: group });
      expect(
        within(list)
          .getAllByRole("link")
          .map((link) => link.textContent),
      ).toEqual(sections);
    }
  });

  it("links Templates to the view inside Agents and marks it there", () => {
    renderNav("/console/orgs/acme/agents/templates");

    expect(screen.getByRole("link", { name: "Templates" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/agents/templates",
    );
    expect(screen.getByRole("link", { name: "Templates" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    // Control: the section the view lives in is a different entry, and
    // is current on its own pages.
    expect(screen.getByRole("link", { name: "Agents" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("marks Agents, not Templates, on one agent's page", () => {
    renderNav("/console/orgs/acme/agents/a1");

    expect(screen.getByRole("link", { name: "Agents" })).toHaveAttribute(
      "aria-current",
      "location",
    );
    expect(screen.getByRole("link", { name: "Templates" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("marks Templates as the location on one template's page", () => {
    renderNav("/console/orgs/acme/agents/templates/t1");

    expect(screen.getByRole("link", { name: "Templates" })).toHaveAttribute(
      "aria-current",
      "location",
    );
  });

  it("offers no sections for an organization the caller does not hold", () => {
    renderNav("/console/orgs/globex/teams");

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("keeps the sections when the organizations list was not answered", () => {
    // Control for the case above: an unanswered list says nothing about
    // whether the caller holds the organization.
    renderNav("/console/orgs/globex/teams", null);

    expect(screen.getByRole("link", { name: "Teams" })).toHaveAttribute(
      "href",
      "/console/orgs/globex/teams",
    );
  });

  it("renders nothing until an organization is selected", () => {
    renderNav("/console/orgs");

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
