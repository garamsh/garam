import { act, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConsoleNav } from "@/app/console/_components/console-nav";
import { ReportOrgShell } from "@/app/console/_components/org-shell";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";

const route = vi.hoisted(() => ({ pathname: "/console/orgs" }));

vi.mock("next/navigation", () => ({ usePathname: () => route.pathname }));

const ORGS = [{ slug: "acme", name: "Acme" }];

function renderNav(
  pathname: string,
  orgs: { slug: string; name: string }[] | null = ORGS,
  hidden?: string[],
) {
  route.pathname = pathname;
  return render(
    <TooltipProvider>
      <SidebarProvider>
        {hidden && <ReportOrgShell org="acme" role={null} hidden={hidden} />}
        <ConsoleNav orgs={orgs} />
      </SidebarProvider>
    </TooltipProvider>,
  );
}

describe("console navigation", () => {
  it("links every section of the selected organization", () => {
    renderNav("/console/orgs/acme");

    expect(screen.getByRole("link", { name: "Home" })).toHaveAttribute(
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

  it("lists Home, Projects, then the organization (ADR-0116)", () => {
    renderNav("/console/orgs/acme");

    const nav = screen.getByRole("navigation", {
      name: "Organization sections",
    });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual([
      "Home",
      "Projects",
      "People",
      "Agents",
      "Teams",
      "Permissions",
      "Operators",
      "Hosts",
      "Settings",
    ]);
    expect(
      within(screen.getByRole("list", { name: "Organization" }))
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual([
      "People",
      "Agents",
      "Teams",
      "Permissions",
      "Operators",
      "Hosts",
      "Settings",
    ]);
  });

  it("leaves out for a reader who can see little what the server denies them, and says so once", async () => {
    await act(async () => {
      renderNav("/console/orgs/acme", ORGS, [
        "agents",
        "teams",
        "operators",
        "hosts",
        "permissions",
        "settings",
      ]);
    });

    const nav = screen.getByRole("navigation", {
      name: "Organization sections",
    });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual(["Home", "Projects", "People"]);
    expect(
      screen.getByText(
        "Agents, Teams, Permissions, Operators, Hosts and Settings hidden: no policy lets you read them.",
      ),
    ).toBeInTheDocument();
  });

  it("says one hidden section in the singular", async () => {
    await act(async () => {
      renderNav("/console/orgs/acme", ORGS, ["permissions"]);
    });

    expect(
      screen.getByText("Permissions hidden: no policy lets you read it."),
    ).toBeInTheDocument();
  });

  it("lists every section and no note before the server has answered", () => {
    // Control for the two above: with no report, nothing is left out.
    renderNav("/console/orgs/acme");

    expect(
      screen.getByRole("link", { name: "Permissions" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/hidden: no policy/)).not.toBeInTheDocument();
  });

  it("marks Agents as the location on one agent's page", () => {
    renderNav("/console/orgs/acme/agents/a1");

    expect(screen.getByRole("link", { name: "Agents" })).toHaveAttribute(
      "aria-current",
      "location",
    );
    expect(screen.getByRole("link", { name: "People" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("lists no Consents entry, though the section stays (ADR-0116)", () => {
    renderNav("/console/orgs/acme/consents");

    expect(
      screen.queryByRole("link", { name: "Consents" }),
    ).not.toBeInTheDocument();
    // The control: the sidebar rendered, with the listed sections.
    expect(screen.getByRole("link", { name: "Settings" })).toBeInTheDocument();
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
