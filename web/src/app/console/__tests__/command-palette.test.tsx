import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { CommandPalette } from "@/app/console/_components/command-palette";
import { stubLocalStorage } from "@/testing/storage";

const route = vi.hoisted(() => ({
  pathname: "/console/orgs/acme",
  push: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  usePathname: () => route.pathname,
  useRouter: () => ({ push: route.push }),
}));

// cmdk measures its list with ResizeObserver, which jsdom lacks.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

const orgs = [
  { slug: "acme", name: "Acme" },
  { slug: "globex", name: "Globex" },
];

beforeEach(() => {
  route.push.mockClear();
  stubLocalStorage();
});

function renderPalette() {
  return render(
    <>
      <input aria-label="Team name" />
      <CommandPalette orgs={orgs} />
    </>,
  );
}

describe("command palette", () => {
  it("puts nothing but its button on the page while closed", () => {
    renderPalette();

    expect(
      screen.getByRole("button", { name: /Search or jump to/ }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("opens on ⌘K and on Ctrl+K", async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.keyboard("{Meta>}k{/Meta}");
    expect(
      screen.getByRole("dialog", { name: "Search or jump to" }),
    ).toBeInTheDocument();

    await user.keyboard("{Escape}");
    await user.keyboard("{Control>}k{/Control}");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("opens on / except while text is being typed", async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.click(screen.getByRole("textbox", { name: "Team name" }));
    await user.keyboard("/");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Team name" })).toHaveValue("/");

    // Control: the same key away from a field opens it.
    await user.click(document.body);
    await user.keyboard("/");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("jumps to a section of the organization on screen", async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.click(screen.getByRole("button", { name: /Search or jump to/ }));
    await user.type(screen.getByRole("combobox"), "templates");
    await user.keyboard("{Enter}");

    expect(route.push).toHaveBeenCalledWith(
      "/console/orgs/acme/agents/templates",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("switches organization", async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.click(screen.getByRole("button", { name: /Search or jump to/ }));
    await user.click(screen.getByRole("option", { name: "Globex" }));

    expect(route.push).toHaveBeenCalledWith("/console/orgs/globex");
  });

  it("offers no sections outside an organization", async () => {
    const user = userEvent.setup();
    route.pathname = "/console/orgs";
    renderPalette();

    await user.click(screen.getByRole("button", { name: /Search or jump to/ }));

    expect(
      screen.queryByRole("option", { name: "Teams" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Acme" })).toBeInTheDocument();
    route.pathname = "/console/orgs/acme";
  });

  it("changes the theme", async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.click(screen.getByRole("button", { name: /Search or jump to/ }));
    await user.click(screen.getByRole("option", { name: "Use dark theme" }));

    expect(document.documentElement).toHaveClass("dark");
  });
});
