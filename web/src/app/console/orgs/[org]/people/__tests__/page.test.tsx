import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PeoplePage from "@/app/console/orgs/[org]/people/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, aliceMembership, signedOut } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

function params(org: string) {
  return { params: Promise.resolve({ org }) };
}

/** What `/orgs/acme/members` answers with, for one test. */
function membersAre(...members: unknown[]) {
  return http.get(`${API_URL}/orgs/acme/members`, () =>
    HttpResponse.json(members),
  );
}

describe("people section", () => {
  it("shows the member's name over the identifier, and no GRN", async () => {
    const { container } = await renderServerComponent(
      PeoplePage(params("acme")),
    );

    // Presence: a row's accessible name is every cell in it, so the
    // name of the member is one part of a longer string.
    const row = screen.getByRole("row", { name: /alice/i });
    expect(row).toHaveTextContent("owner");
    expect(row).toHaveTextContent("2026-01-02");
    // Exact matches: the fixture's name and its identifier segment
    // differ only in case, so a loose match cannot tell them apart.
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("says there is no name rather than putting the subject in its place", async () => {
    server.use(
      membersAre({
        userGrn: "grn:root:default:user:112727439469584391833",
        role: "owner",
        since: "2026-01-02T03:04:05Z",
      }),
    );

    const gone = await renderServerComponent(PeoplePage(params("acme")));

    expect(
      within(gone.container).getByText("No name on record"),
    ).toBeInTheDocument();
    expect(
      within(gone.container).getByText("112727439469584391833"),
    ).toBeInTheDocument();

    // The control: the same row carrying a nickname names the person
    // and drops the phrase, so what the phrase above reports is the
    // absent nickname rather than something the cell always renders.
    server.use(membersAre(aliceMembership));

    const named = await renderServerComponent(PeoplePage(params("acme")));

    expect(within(named.container).getByText("Alice")).toBeInTheDocument();
    expect(
      within(named.container).queryByText("No name on record"),
    ).not.toBeInTheDocument();
  });

  it("shows an empty state rather than a bare table", async () => {
    server.use(membersAre());

    await renderServerComponent(PeoplePage(params("acme")));

    expect(
      screen.getByText("No one belongs to this organization."),
    ).toBeInTheDocument();
  });

  it("offers invitations to a plain member, for the API to refuse", async () => {
    server.use(
      membersAre({
        userGrn: "grn:root:default:user:alice",
        nickname: "Alice",
        role: "member",
        since: "2026-01-02T03:04:05Z",
      }),
    );

    await renderServerComponent(PeoplePage(params("acme")));

    // The control for the presence: the role the console once gated on
    // is the one rendered here, so the form standing is the absence of
    // that gate rather than a page that failed to render at all. The
    // regex is presence, as above.
    expect(screen.getByRole("row", { name: /alice/i })).toHaveTextContent(
      "member",
    );
    expect(
      screen.getByRole("heading", { name: "Invite a member" }),
    ).toBeInTheDocument();
  });

  it("refuses a body the specification does not describe", async () => {
    server.use(
      membersAre({
        userGrn: "root:default:user:alice",
        role: "owner",
        since: "2026-01-02T03:04:05Z",
      }),
    );

    await expect(
      renderServerComponent(PeoplePage(params("acme"))),
    ).rejects.toThrow(/outside its specification/);

    // The control: the same read against the body the API answers with
    // renders, so the refusal above is the GRN and not a parser that
    // refuses every member.
    server.use(membersAre(aliceMembership));

    await renderServerComponent(PeoplePage(params("acme")));

    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it.each([403, 404])("renders not-found on %i", async (status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status }),
      ),
    );

    await expect(
      renderServerComponent(PeoplePage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than reporting no people", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(PeoplePage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("No one belongs to this organization."),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(PeoplePage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
