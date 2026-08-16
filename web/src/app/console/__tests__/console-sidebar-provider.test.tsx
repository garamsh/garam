import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { hydrateRoot } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ConsoleSidebarProvider,
} from "@/app/console/_components/console-sidebar-provider";
import { Sidebar, SidebarTrigger } from "@/components/ui/sidebar";

const shell = (
  <ConsoleSidebarProvider>
    <Sidebar collapsible="icon" />
    <SidebarTrigger />
  </ConsoleSidebarProvider>
);

// A page load, not a client render: the server has no cookie to read, so
// only hydration can tell the sidebar what the last visit left it as.
async function loadShell() {
  const container = document.createElement("div");
  container.innerHTML = renderToString(shell);
  document.body.append(container);
  const servedState = container
    .querySelector('[data-slot="sidebar"]')
    ?.getAttribute("data-state");

  await act(async () => {
    hydrateRoot(container, shell);
  });

  return {
    servedState,
    sidebar: container.querySelector('[data-slot="sidebar"]'),
  };
}

describe("console sidebar provider", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.replaceChildren();
    document.cookie = "sidebar_state=; path=/; max-age=0";
  });

  it("opens the sidebar when no visit has stored one", async () => {
    const { sidebar } = await loadShell();

    expect(sidebar).toHaveAttribute("data-state", "expanded");
  });

  it("collapses the sidebar the last visit left collapsed", async () => {
    document.cookie = "sidebar_state=false; path=/";
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});

    const { servedState, sidebar } = await loadShell();

    expect(servedState).toBe("expanded");
    expect(sidebar).toHaveAttribute("data-state", "collapsed");
    // A hydration mismatch is what a cookie read at render time would
    // cost; React reports it here.
    expect(logged).not.toHaveBeenCalled();
  });

  it("follows the trigger after the stored state arrives", async () => {
    document.cookie = "sidebar_state=false; path=/";
    const { sidebar } = await loadShell();

    await userEvent.click(
      screen.getByRole("button", { name: "Toggle Sidebar" }),
    );

    expect(sidebar).toHaveAttribute("data-state", "expanded");
    expect(document.cookie).toContain("sidebar_state=true");
  });
});
