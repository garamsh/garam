import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
        {hidden && (
          <ReportOrgShell org="acme" role={null} hidden={hidden} objects={[]} />
        )}
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

  it("lists Home, the members, Projects, then the organization (ADR-0113)", () => {
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
      "People",
      "Agents",
      "Teams",
      "Projects",
      "Permissions",
      "Operators",
      "Templates",
      "Consents",
      "Settings",
    ]);
    for (const [group, sections] of Object.entries({
      Members: ["People", "Agents", "Teams"],
      Organization: [
        "Permissions",
        "Operators",
        "Templates",
        "Consents",
        "Settings",
      ],
    })) {
      const list = screen.getByRole("list", { name: group });
      expect(
        within(list)
          .getAllByRole("link")
          .map((link) => link.textContent),
      ).toEqual(sections);
    }
  });

  it("leaves out for a reader who can see little what the server denies them, and says so once", async () => {
    await act(async () => {
      renderNav("/console/orgs/acme", ORGS, [
        "teams",
        "agents/templates",
        "operators",
        "permissions",
        "consents",
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
    ).toEqual(["Home", "People", "Agents", "Projects"]);
    // A group with nothing left to list goes with its label.
    expect(
      screen.queryByRole("list", { name: "Organization" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(
        /^Teams, Permissions, Operators, Templates, Consents and Settings are not shown: no policy lets you read them\./,
      ),
    ).toBeInTheDocument();
  });

  it("explains in place why a section is not shown", async () => {
    await act(async () => {
      renderNav("/console/orgs/acme", ORGS, ["permissions"]);
    });

    expect(
      screen.getByText(/^Permissions is not shown: no policy lets you read/),
    ).toBeInTheDocument();
    const why = screen.getByRole("button", { name: "Why" });
    expect(why).toHaveAttribute("aria-expanded", "false");

    await userEvent.click(why);

    expect(why).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByText(/roles and policies let you read what it holds/),
    ).toBeInTheDocument();
  });

  it("lists every section and no note before the server has answered", () => {
    // Control for the two above: with no report, nothing is left out.
    renderNav("/console/orgs/acme");

    expect(
      screen.getByRole("link", { name: "Permissions" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Why" }),
    ).not.toBeInTheDocument();
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
