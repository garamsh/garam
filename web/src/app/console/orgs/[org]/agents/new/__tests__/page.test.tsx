import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import NewAgentPage from "@/app/console/orgs/[org]/agents/new/page";
import { renderServerComponent } from "@/testing/render";
import { researcher } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function page() {
  return NewAgentPage({ params: Promise.resolve({ org: "acme" }) });
}

describe("new agent page", () => {
  it("offers no form that sends garam a definition or a template", async () => {
    // garam accepts no desired value (ADR-0090) and its templates are
    // retired read-only (ADR-0088): the page reads neither and offers
    // no field to enter a value into.
    const read: string[] = [];
    server.events.on("request:start", ({ request }) => {
      read.push(new URL(request.url).pathname);
    });
    const { container } = await renderServerComponent(page());
    server.events.removeAllListeners();

    expect(read).not.toContain("/orgs/acme/agent-templates");
    expect(read).not.toContain("/orgs/acme/agent-definitions");
    expect(container.textContent).not.toContain(researcher.name);
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { level: 2, name: "Define in garam" }),
    ).not.toBeInTheDocument();
    // The control: the page did read garam, for the operators whose
    // control service it creates through, and names that section.
    expect(read).toContain("/orgs/acme/operators");
    expect(
      screen.getByRole("heading", {
        level: 2,
        name: "From the control service",
      }),
    ).toBeInTheDocument();
  });
});
