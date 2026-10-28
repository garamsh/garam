import { describe, expect, it, vi } from "vitest";

import { ConsoleSidebarProvider } from "@/app/console/_components/console-sidebar-provider";
import { Sidebar } from "@/components/ui/sidebar";
import { renderServerComponent } from "@/testing/render";

const stored = vi.hoisted(() => ({ value: undefined as string | undefined }));

// The setup file answers every cookie with the session token; the
// sidebar needs one that carries its own value, or none at all.
vi.mock("next/headers", () => ({
  cookies: () =>
    Promise.resolve({
      get: (name: string) =>
        name === "sidebar_state" && stored.value !== undefined
          ? { name, value: stored.value }
          : undefined,
    }),
}));

async function renderShell() {
  const { container } = await renderServerComponent(
    ConsoleSidebarProvider({ children: <Sidebar collapsible="icon" /> }),
  );
  return container.querySelector('[data-slot="sidebar"]');
}

describe("console sidebar provider", () => {
  it("serves the sidebar expanded when no visit has stored one", async () => {
    stored.value = undefined;

    expect(await renderShell()).toHaveAttribute("data-state", "expanded");
  });

  it("serves the sidebar collapsed when the last visit collapsed it", async () => {
    stored.value = "false";

    expect(await renderShell()).toHaveAttribute("data-state", "collapsed");
  });
});
