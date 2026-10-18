import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PeoplePage from "@/app/console/orgs/[org]/people/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  aliceMembership,
  answerPermissionChecks,
  signedOut,
} from "@/testing/mocks/handlers";
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

/** The page under the tooltip provider the console layout mounts. */
function renderPeople() {
  return renderServerComponent(PeoplePage(params("acme")), {
    wrapper: TooltipProvider,
  });
}

/** What `/orgs/acme/members` answers with, for one test. */
function membersAre(...members: unknown[]) {
  return http.get(`${API_URL}/orgs/acme/members`, () =>
    HttpResponse.json(members),
  );
}

describe("people section", () => {
  it("shows the member's name over the identifier, and no GRN", async () => {
    const { container } = await renderPeople();

    // Presence: a row's accessible name is every cell in it, so the
    // name of the member is one part of a longer string.
    const row = screen.getByRole("row", { name: /alice/i });
    expect(within(row).getByText("Owner")).toBeInTheDocument();
    // The joining time is phrased, with the instant kept on the
    // element rather than printed as an ISO date.
    expect(row.querySelector("time")).toHaveAttribute(
      "datetime",
      aliceMembership.since.replace("Z", ".000Z"),
    );
    expect(row).not.toHaveTextContent("2026-01-02");
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

    const gone = await renderPeople();

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

    const named = await renderPeople();

    expect(within(named.container).getByText("Alice")).toBeInTheDocument();
    expect(
      within(named.container).queryByText("No name on record"),
    ).not.toBeInTheDocument();
  });

  it("shows an empty state rather than a bare table", async () => {
    server.use(membersAre());

    await renderPeople();

    expect(
      screen.getByText("No one belongs to this organization."),
    ).toBeInTheDocument();
  });

  it("leaves out the invitation only when the server denies it", async () => {
    // The control: an answer the server cannot give still offers it.
    server.use(answerPermissionChecks(() => "unknown"));
    const unknown = await renderPeople();
    expect(
      screen.getByRole("button", { name: "Invite people" }),
    ).toBeInTheDocument();
    unknown.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "org:invite" ? "deny" : "allow",
      ),
    );
    await renderPeople();

    expect(
      screen.queryByRole("button", { name: "Invite people" }),
    ).not.toBeInTheDocument();
    // The list stays; only the action went.
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it("refuses a body the specification does not describe", async () => {
    server.use(
      membersAre({
        userGrn: "root:default:user:alice",
        role: "owner",
        since: "2026-01-02T03:04:05Z",
      }),
    );

    await expect(renderPeople()).rejects.toThrow(/outside its specification/);

    // The control: the same read against the body the API answers with
    // renders, so the refusal above is the GRN and not a parser that
    // refuses every member.
    server.use(membersAre(aliceMembership));

    await renderPeople();

    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it.each([403, 404])(
    "renders not-found when the organization answers %i",
    async (status) => {
      server.use(
        http.get(`${API_URL}/orgs/acme`, () =>
          HttpResponse.json({ kind: "denied", message: "no" }, { status }),
        ),
      );

      await expect(renderPeople()).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the list is refused rather than showing no one", async () => {
    // The control: answered, the list shows its member.
    const allowed = await renderPeople();
    expect(screen.getByText("Alice")).toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await renderPeople();

    expect(screen.getByRole("status")).toHaveTextContent(
      // Presence: the notice goes on to say where access comes from.
      /^You can't view this organization's people/,
    );
    expect(
      screen.queryByText("No one belongs to this organization."),
    ).not.toBeInTheDocument();
  });

  it("says the API is unreachable rather than reporting no people", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderPeople();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("No one belongs to this organization."),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(renderPeople()).rejects.toThrow("NEXT_REDIRECT:/login");
  });
  it("leaves out the invitation on a refused list only when denied", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );
    // The control: refused the list but allowed to invite, the action
    // stays beside the notice.
    const allowed = await renderPeople();
    expect(
      screen.getByRole("button", { name: "Invite people" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "org:invite" ? "deny" : "allow",
      ),
    );
    await renderPeople();

    expect(screen.getByRole("status")).toHaveTextContent(
      // Presence: the notice goes on to say where access comes from.
      /^You can't view this organization's people/,
    );
    expect(
      screen.queryByRole("button", { name: "Invite people" }),
    ).not.toBeInTheDocument();
  });
});
