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
  it("offers no garam template and defines from a blank form", async () => {
    // garam's templates are retired read-only (ADR-0088): the page does
    // not read them, so a template garam still answers is not offered.
    const read: string[] = [];
    server.events.on("request:start", ({ request }) => {
      read.push(new URL(request.url).pathname);
    });
    const { container } = await renderServerComponent(page());
    server.events.removeAllListeners();

    expect(read).not.toContain("/orgs/acme/agent-templates");
    expect(
      screen.queryByRole("navigation", { name: "Start from" }),
    ).not.toBeInTheDocument();
    expect(container.textContent).not.toContain(researcher.name);
    expect(screen.getByLabelText("Key 1")).toHaveValue("");
    // The control: the page did read garam, for the operators the form
    // offers by name, so the template's absence is not a page that read
    // nothing.
    expect(read).toContain("/orgs/acme/operators");
    expect(
      screen.getByRole("option", { name: "Field cluster one (field-one)" }),
    ).toBeInTheDocument();
  });

  it("names the control service as where templates are created from", async () => {
    await renderServerComponent(page());

    expect(
      screen.getByRole("heading", {
        level: 2,
        name: "From the control service",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: "Define in garam" }),
    ).toBeInTheDocument();
  });

  it("labels the settings as saved values without claiming they are applied", async () => {
    const { container } = await renderServerComponent(page());

    expect(screen.getByText("Advanced settings")).toBeInTheDocument();
    // Presence: the sentence goes on to warn about credentials.
    expect(
      screen.getByText(/Saved values; application is not reported\./),
    ).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/is running|deployed|Sherlock/i);
  });
});
