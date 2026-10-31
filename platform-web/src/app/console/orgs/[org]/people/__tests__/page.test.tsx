import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import {
  MemberList,
  type MemberFilters,
  type MemberRow,
} from "@/app/console/_components/member-list";
import PeoplePage from "@/app/console/orgs/[org]/people/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import { API_URL, answerPermissionChecks } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

const route = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/people",
  useRouter: () => ({ push: route.push, refresh: () => {} }),
}));

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

// cmdk-free, but Radix Select measures with ResizeObserver.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

/** The two people the list test acts on, as the page decides them. */
const ALICE_ROW: MemberRow = {
  kind: "person",
  grn: "grn:root:default:user:alice",
  identifier: "alice",
  name: "Alice",
  href: "/console/orgs/acme/people/alice",
  status: { tone: "neutral", label: "Owner" },
  detail: null,
  runsOn: null,
  lastSeen: null,
};
const BOB_ROW: MemberRow = {
  ...ALICE_ROW,
  grn: "grn:root:default:user:bob",
  identifier: "bob",
  name: "Bob",
  href: "/console/orgs/acme/people/bob",
  status: { tone: "neutral", label: "Member" },
};
const FILTERS: MemberFilters = { team: "", runs: "", teams: [], runsOn: null };

function renderPeople(query: Record<string, string> = {}) {
  return renderServerComponent(
    PeoplePage({
      params: Promise.resolve({ org: "acme" }),
      searchParams: Promise.resolve(query),
    }),
    { wrapper: TooltipProvider },
  );
}

function refuse(path: string) {
  return http.get(`${API_URL}/orgs/acme/${path}`, () =>
    HttpResponse.json(
      { kind: "permission_denied", message: "no" },
      { status: 403 },
    ),
  );
}

/** The table's rows' member names, in order. */
function names(): string[] {
  const table = screen.getByRole("table", { name: "People" });
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map(
      (row) =>
        within(row)
          .getAllByRole("link")
          .find((link) => link.getAttribute("href")?.startsWith("/console"))
          ?.textContent ?? "",
    );
}

describe("people", () => {
  it("lists the organization's people alone, each linked to their page (ADR-0116)", async () => {
    await renderPeople();

    // The organization also holds agents; they are Agents' to list.
    expect(names()).toEqual(["Alice"]);
    expect(screen.getByRole("link", { name: "Alice" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/people/alice",
    );
    expect(screen.getByRole("columnheader", { name: /Role/ })).toBeVisible();
    expect(
      screen.queryByRole("columnheader", { name: /Runs on/ }),
    ).not.toBeInTheDocument();
  });

  it("says a refused list of people, never an empty one", async () => {
    server.use(refuse("members"));

    await renderPeople();

    expect(
      screen.getByText("You can't view this organization's people"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("says where a team filter could not be read, and lists everyone", async () => {
    server.use(refuse("groups/eng/members"));

    await renderPeople({ team: "eng" });

    expect(
      screen.getByText("You can't view this team's members"),
    ).toBeInTheDocument();
    expect(names()).toEqual(["Alice"]);
  });

  it("keeps inviting on the page, inactive with its reason, where the server denies it", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "org:invite" ? "deny" : "allow",
      ),
    );

    await renderPeople();

    const invite = screen.getByRole("button", { name: "Invite people" });
    expect(invite).toHaveAttribute("aria-disabled", "true");
    expect(invite).toHaveAccessibleDescription(
      "No policy lets you invite people.",
    );
  });

  it("offers inviting as a working button where the server allows it", async () => {
    // The control for the case above.
    await renderPeople();

    expect(
      screen.getByRole("button", { name: "Invite people" }),
    ).not.toHaveAttribute("aria-disabled");
  });

  describe("pending invitations", () => {
    const ID = "0f8c2a5e-9d1b-4c3a-8e7f-6a5b4c3d2e1f";
    const pendingOne = (createdByGrn: string) =>
      http.get(`${API_URL}/orgs/acme/invitations`, () =>
        HttpResponse.json([
          {
            id: ID,
            role: "admin",
            createdByGrn,
            createdAt: "2026-01-02T03:04:05Z",
            expiresAt: "2026-01-09T03:04:05Z",
          },
        ]),
      );

    it("names who sent each one and offers revoking it (ADR-0136)", async () => {
      const revoked: string[] = [];
      server.use(
        pendingOne(ALICE_ROW.grn),
        http.delete(
          `${API_URL}/orgs/acme/invitations/:invitation`,
          ({ params }) => {
            revoked.push(String(params.invitation));
            return new HttpResponse(null, { status: 204 });
          },
        ),
      );
      const user = userEvent.setup();

      await renderPeople();

      const table = screen.getByRole("table", { name: "Pending invitations" });
      const row = within(table).getAllByRole("row")[1];
      expect(within(row).getByText("Admin")).toBeInTheDocument();
      expect(within(row).getByText("Alice")).toBeInTheDocument();
      await user.click(within(row).getByRole("button", { name: "Revoke" }));
      const dialog = screen.getByRole("alertdialog", {
        name: "Revoke this invitation?",
      });
      await user.click(
        within(dialog).getByRole("button", { name: "Revoke invitation" }),
      );

      await waitFor(() => expect(revoked).toEqual([ID]));
    });

    it("names an inviter who left by their identifier", async () => {
      server.use(pendingOne("grn:root:default:user:gone"));

      await renderPeople();

      expect(screen.getByText("gone")).toBeInTheDocument();
    });

    it("shows no such part with none pending", async () => {
      await renderPeople();

      expect(
        screen.queryByRole("region", { name: /Pending invitations/ }),
      ).not.toBeInTheDocument();
    });

    it("does not read them for a caller the server denies inviting", async () => {
      let read = false;
      server.use(
        answerPermissionChecks((action) =>
          action === "org:invite" ? "deny" : "allow",
        ),
        http.get(`${API_URL}/orgs/acme/invitations`, () => {
          read = true;
          return HttpResponse.json([]);
        }),
      );

      await renderPeople();

      expect(read).toBe(false);
      expect(
        screen.queryByText(/pending invitations/i),
      ).not.toBeInTheDocument();
    });

    it("says a list garam never answered, rather than leaving it out", async () => {
      server.use(
        http.get(`${API_URL}/orgs/acme/invitations`, () =>
          HttpResponse.error(),
        ),
      );

      await renderPeople();

      expect(screen.getByRole("alert").textContent).toMatch(
        // Presence: the banner's sentence wraps the subject.
        /pending invitations/,
      );
    });

    it("says a refused list, where the check let the read through", async () => {
      server.use(refuse("invitations"));

      await renderPeople();

      expect(
        screen.getByText(
          "You can't view this organization's pending invitations",
        ),
      ).toBeInTheDocument();
    });
  });

  it("adds the chosen people to a team, one request each", async () => {
    const added: string[] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/groups/:group/members`,
        async ({ request, params }) => {
          const { memberGrn } = (await request.json()) as {
            memberGrn: string;
          };
          added.push(`${String(params.group)} ${memberGrn}`);
          return HttpResponse.json({ memberGrn }, { status: 201 });
        },
      ),
    );
    const user = userEvent.setup();
    // The list alone: what the page reads to build it is the other
    // tests' to assert, and rendering the whole page here was most of
    // this test's time on a loaded host (#1551).
    render(
      <TooltipProvider>
        <MemberList
          org="acme"
          kind="person"
          rows={[ALICE_ROW, BOB_ROW]}
          filters={FILTERS}
          teamChoices={[{ value: "eng", label: "Engineering" }]}
          policyChoices={[]}
          empty={null}
        />
      </TooltipProvider>,
    );

    await user.click(screen.getByRole("checkbox", { name: "Select Alice" }));
    await user.click(screen.getByRole("checkbox", { name: "Select Bob" }));
    expect(screen.getByText("2 selected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add to team" }));
    const dialog = screen.getByRole("alertdialog", {
      name: "Add Alice and Bob to a team?",
    });
    await user.click(within(dialog).getByRole("combobox", { name: "Team" }));
    await user.click(screen.getByRole("option", { name: "Engineering" }));
    await user.click(
      within(dialog).getByRole("button", { name: "Add to team" }),
    );

    expect(await screen.findByText("Added 2 members")).toBeInTheDocument();
    expect(added).toEqual([
      "eng grn:root:default:user:alice",
      "eng grn:root:default:user:bob",
    ]);
  });

  it("offers no team the server denies adding to", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "group:member-add" ? "deny" : "allow",
      ),
    );

    await renderPeople();

    // Engineering is the only team, so adding is kept, inactive.
    expect(screen.getByRole("button", { name: "Add to team" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });
});
