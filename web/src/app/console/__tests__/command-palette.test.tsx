import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ActionDialog } from "@/app/console/_components/action-dialog";
import { CommandPalette } from "@/app/console/_components/command-palette";
import { ReportOrgShell } from "@/app/console/_components/org-shell";
import { Button } from "@/components/ui/button";
import { API_URL, aliceMembership } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
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

  for (const [key, keys] of [
    ["/", "/"],
    ["Ctrl+K", "{Control>}k{/Control}"],
  ] as const) {
    it(`does not open on ${key} over an alert dialog`, async () => {
      const user = userEvent.setup();
      // Loaded up front, so a dialog that wrongly opened would be on
      // screen within the wait below rather than still loading.
      await import("@/app/console/_components/command-palette-dialog");
      const { rerender } = render(
        <>
          <div role="alertdialog" aria-label="Save the operator key" />
          <CommandPalette orgs={orgs} />
        </>,
      );

      await user.keyboard(keys);
      await expect(
        screen.findByRole("dialog", undefined, { timeout: 300 }),
      ).rejects.toThrow();

      // Control: the same key opens it once the alert dialog is gone.
      rerender(<CommandPalette orgs={orgs} />);
      await user.keyboard(keys);
      expect(
        await screen.findByRole("dialog", { name: "Search or jump to" }),
      ).toBeInTheDocument();
    });
  }

  it("jumps to a section of the organization on screen", async () => {
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    await user.type(screen.getByRole("combobox"), "consents");
    await user.keyboard("{Enter}");

    // Consents has no sidebar entry (ADR-0116); the palette reaches it.
    expect(route.push).toHaveBeenCalledWith("/console/orgs/acme/consents");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("matches what was typed as written, not scattered letters", async () => {
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    await user.type(screen.getByRole("combobox"), "cons");
    expect(screen.getByRole("option", { name: "Consents" })).toBeVisible();

    await user.clear(screen.getByRole("combobox"));
    await user.type(screen.getByRole("combobox"), "cnsts");
    expect(
      screen.queryByRole("option", { name: "Consents" }),
    ).not.toBeInTheDocument();
    // The organization's objects are searched too, so the answer waits
    // on that search.
    expect(
      await screen.findByText("Nothing matches that."),
    ).toBeInTheDocument();
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

  it("searches the organization's objects by name once something is typed, and reads none before", async () => {
    const asked: string[] = [];
    server.events.on("request:start", ({ request }) => {
      asked.push(new URL(request.url).pathname);
    });
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    // Past the 200 ms the palette lets typing rest before it asks, so
    // a search it ran without anything typed would have been sent.
    await act(() => new Promise((resolve) => setTimeout(resolve, 400)));
    // Found, not browsed: nothing typed reads and lists no object.
    expect(asked).toEqual([]);
    expect(
      screen.queryByRole("option", { name: /Scout/ }),
    ).not.toBeInTheDocument();

    await user.type(screen.getByRole("combobox"), "sco");
    // Presence: the option's name is the object's and its identifier.
    expect(await screen.findByRole("option", { name: /Scout/ })).toBeVisible();
    expect(
      screen.queryByRole("option", { name: /Engineering/ }),
    ).not.toBeInTheDocument();
    await user.keyboard("{Enter}");

    expect(route.push).toHaveBeenCalledWith(
      "/console/orgs/acme/agents/9f2ac1b40d8e7a35",
    );
  });

  it("answers a few of each kind and links the list for the rest", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json(
          Array.from({ length: 7 }, (_, index) => ({
            ...aliceMembership,
            userGrn: `grn:root:default:user:sam-${index}`,
            nickname: `Sam ${index}`,
          })),
        ),
      ),
    );
    const user = userEvent.setup();
    renderPalette();

    await openPalette(user);
    await user.type(screen.getByRole("combobox"), "sam");

    await screen.findByRole("option", { name: /^Sam 0/ });
    expect(screen.getAllByRole("option", { name: /^Sam \d/ })).toHaveLength(5);
    await user.click(screen.getByRole("option", { name: "2 more in people…" }));

    expect(route.push).toHaveBeenCalledWith("/console/orgs/acme/people");
  });

  it("offers no section the server denies the caller", async () => {
    const user = userEvent.setup();
    render(
      <>
        <ReportOrgShell org="acme" role={null} hidden={["permissions"]} />
        <CommandPalette orgs={orgs} />
      </>,
    );

    await openPalette(user);

    expect(
      screen.queryByRole("option", { name: "Permissions" }),
    ).not.toBeInTheDocument();
    // Control: a section the server allows is still offered.
    expect(screen.getByRole("option", { name: "Teams" })).toBeInTheDocument();
  });

  it("opens on . with the page's actions alone, each with its consequence", async () => {
    const user = userEvent.setup();
    const action = vi.fn(async () => ({ status: "idle" }));
    render(
      <>
        <input aria-label="Team name" />
        <ActionDialog
          trigger={<Button type="button">Delete team</Button>}
          title="Delete Engineering?"
          description={<p>This removes the team.</p>}
          action={action}
          initialState={{ status: "idle" }}
          fields={{}}
          confirmLabel="Delete team"
          pendingLabel="Deleting…"
          done={() => false}
          command={{
            label: "Delete team",
            consequence: "Removes the team and its memberships.",
          }}
        />
        <CommandPalette orgs={orgs} />
      </>,
    );

    await user.click(screen.getByRole("textbox", { name: "Team name" }));
    await user.keyboard(".");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    // Control: the same key away from a field opens it.
    await user.click(document.body);
    await user.keyboard(".");
    const palette = await screen.findByRole("dialog", {
      name: "Actions on this page",
    });
    expect(palette).toHaveTextContent("Removes the team and its memberships.");
    expect(
      screen.queryByRole("option", { name: "Use dark theme" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("option", { name: /^Delete team/ }));

    expect(
      await screen.findByRole("alertdialog", { name: "Delete Engineering?" }),
    ).toBeInTheDocument();
    expect(action).not.toHaveBeenCalled();
  });

  it("says when the page offers no actions", async () => {
    const user = userEvent.setup();
    renderPalette();

    await user.keyboard(".");

    expect(
      await screen.findByText("This page offers no actions here."),
    ).toBeInTheDocument();
  });
});
