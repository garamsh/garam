import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import ConsoleLayout from "@/app/console/layout";
import { SidebarProvider } from "@garam/design/ui/sidebar";

// react-dom cannot render the shell's async Server Components under
// Testing Library. A thrown promise stands in for their pending reads
// so the test controls the suspension.
const shell = vi.hoisted(() => ({
  pending: undefined as Promise<void> | undefined,
}));

vi.mock("@/app/console/_components/console-sidebar", () => ({
  ConsoleSidebar: () => {
    if (shell.pending) throw shell.pending;
    return <p>Sidebar</p>;
  },
  ConsoleSidebarFallback: () => <p>Sidebar loading</p>,
}));

vi.mock("@/app/console/_components/console-breadcrumb", () => ({
  ConsoleBreadcrumb: () => {
    if (shell.pending) throw shell.pending;
    return <p>Breadcrumb</p>;
  },
}));

vi.mock("@/app/console/_components/console-command", () => ({
  ConsoleCommand: () => {
    if (shell.pending) throw shell.pending;
    return <p>Command palette</p>;
  },
}));

// The palette's fallback is the client component itself, which reads
// the router.
vi.mock("next/navigation", () => ({
  usePathname: () => "/console/orgs",
  useRouter: () => ({ push: () => {} }),
}));

// The provider's own read is a Server Component too; its seeding is
// covered in console-sidebar-provider.test.tsx.
vi.mock("@/app/console/_components/console-sidebar-provider", () => ({
  ConsoleSidebarProvider: ({ children }: { children: React.ReactNode }) => (
    <SidebarProvider>{children}</SidebarProvider>
  ),
}));

afterEach(() => {
  shell.pending = undefined;
});

describe("console layout", () => {
  it("streams children while the shell's reads are still pending", async () => {
    let resolve!: () => void;
    shell.pending = new Promise<void>((settle) => {
      resolve = settle;
    });

    render(
      <ConsoleLayout>
        <p role="status">Loading organizations…</p>
      </ConsoleLayout>,
    );

    // The `loading.tsx` stand-in: children stream immediately instead
    // of waiting on the sidebar's session and organization reads.
    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading organizations…",
    );
    expect(screen.getByText("Sidebar loading")).toBeInTheDocument();
    expect(screen.queryByText("Breadcrumb")).not.toBeInTheDocument();
    expect(screen.queryByText("Command palette")).not.toBeInTheDocument();

    await act(async () => {
      shell.pending = undefined;
      resolve();
    });
    expect(screen.getByText("Sidebar")).toBeInTheDocument();
    expect(screen.getByText("Breadcrumb")).toBeInTheDocument();
    expect(screen.getByText("Command palette")).toBeInTheDocument();
  });

  it("offers the sidebar trigger on every route", () => {
    render(
      <ConsoleLayout>
        <p>Anything</p>
      </ConsoleLayout>,
    );

    expect(
      screen.getByRole("button", { name: "Toggle Sidebar" }),
    ).toBeInTheDocument();
  });
});
