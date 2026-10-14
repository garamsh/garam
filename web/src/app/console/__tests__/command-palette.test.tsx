import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { CommandPalette } from "@/app/console/_components/command-palette";
import { stubLocalStorage } from "@/testing/storage";

const route = vi.hoisted(() => ({
  pathname: "/console/orgs/acme",
  push: vi.fn(),
  dialogLoaded: false,
}));

vi.mock("next/navigation", () => ({
  usePathname: () => route.pathname,
  useRouter: () => ({ push: route.push }),
}));

// Records when the dialog's module is first imported, which is what
// shipping it apart from the page means.
vi.mock("@/app/console/_components/command-palette-dialog", async (load) => {
  route.dialogLoaded = true;
  return await load();
});

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
  route.pathname = "/console/orgs/acme";
  stubLocalStorage();
});

function renderPalette(list: typeof orgs | null = orgs) {
  return render(
    <>
      <input aria-label="Team name" />
      <CommandPalette orgs={list} />
    </>,
  );
}

async function openPalette(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /Search or jump to/ }));
  return screen.findByRole("dialog", { name: "Search or jump to" });
}

describe("command palette", () => {
  it("puts nothing but its button on the page while closed", () => {
    renderPalette();

    expect(
      screen.getByRole("button", { name: /Search or jump to/ }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("loads the dialog's code only once it is opened", async () => {
    const user = userEvent.setup();
    renderPalette();
    expect(route.dialogLoaded).toBe(false);

    await openPalette(user);

    expect(route.dialogLoaded).toBe(true);
  });

  it("opens on ⌘K and on Ctrl+K", async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.keyboard("{Meta>}k{/Meta}");
    expect(
      await screen.findByRole("dialog", { name: "Search or jump to" }),
    ).toBeInTheDocument();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.keyboard("{Control>}k{/Control}");
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
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
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
  });

  it("does not open over an alert dialog", async () => {
    const user = userEvent.setup();
    render(
      <>
        <div role="alertdialog" aria-label="Save the operator key" />
        <CommandPalette orgs={orgs} />
      </>,
    );

    await user.keyboard("/");
    await user.keyboard("{Control>}k{/Control}");

    // The dialog loads asynchronously, so its absence is waited for.
    await expect(
      screen.findByRole("dialog", undefined, { timeout: 300 }),
    ).rejects.toThrow();
  });

  it("jumps to a section of the organization on screen", async () => {
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    await user.type(screen.getByRole("combobox"), "templates");
    await user.keyboard("{Enter}");

    expect(route.push).toHaveBeenCalledWith(
      "/console/orgs/acme/agents/templates",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("matches what was typed as written, not scattered letters", async () => {
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    await user.type(screen.getByRole("combobox"), "temp");
    expect(screen.getByRole("option", { name: "Templates" })).toBeVisible();

    await user.clear(screen.getByRole("combobox"));
    await user.type(screen.getByRole("combobox"), "tmpl");
    expect(
      screen.queryByRole("option", { name: "Templates" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Nothing matches that.")).toBeInTheDocument();
  });

  it("says which keys open, move and close", async () => {
    const user = userEvent.setup();
    renderPalette();

    const dialog = await openPalette(user);

    // Presence: each hint is a key and the word after it.
    expect(dialog).toHaveTextContent(/↵\s*to open/);
    expect(dialog).toHaveTextContent(/esc\s*to close/);
  });

  it("switches organization", async () => {
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    await user.click(screen.getByRole("option", { name: "Globex" }));

    expect(route.push).toHaveBeenCalledWith("/console/orgs/globex");
  });

  it("offers no sections outside an organization", async () => {
    const user = userEvent.setup();
    route.pathname = "/console/orgs";
    renderPalette();

    await openPalette(user);

    expect(
      screen.queryByRole("option", { name: "Teams" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Acme" })).toBeInTheDocument();
  });

  it("offers no sections for an organization the caller does not hold", async () => {
    const user = userEvent.setup();
    route.pathname = "/console/orgs/initech";
    renderPalette();

    await openPalette(user);

    expect(
      screen.queryByRole("option", { name: "Teams" }),
    ).not.toBeInTheDocument();
  });

  it("changes the theme", async () => {
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    await user.click(screen.getByRole("option", { name: "Use dark theme" }));

    expect(document.documentElement).toHaveClass("dark");
  });
});
