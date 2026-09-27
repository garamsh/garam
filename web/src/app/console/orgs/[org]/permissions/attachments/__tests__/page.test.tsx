import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AttachmentsPage from "@/app/console/orgs/[org]/permissions/attachments/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, alice } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const POLICIES = `${API_URL}/orgs/acme/policies`;

function renderAttachments(subject?: string) {
  return renderServerComponent(
    AttachmentsPage({
      params: Promise.resolve({ org: "acme" }),
      searchParams: Promise.resolve(subject === undefined ? {} : { subject }),
    }),
  );
}

function refuse() {
  return HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 });
}

/** The section of the inspection under the heading `name`. */
function section(name: string): HTMLElement {
  return screen.getByRole("region", { name });
}

describe("attachments page", () => {
  it("shows whom each policy is attached to, and everyone for none", async () => {
    await renderAttachments();

    // Presence: a row's accessible name is all of its cells.
    const attached = screen.getByRole("row", { name: /Deny project deletion/ });
    expect(
      within(attached).getByRole("link", { name: "Engineering (Team)" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments?subject=team%3Aeng",
    );
    // Presence, for the same reason.
    const everyone = screen.getByRole("row", { name: /Baseline read/ });
    expect(
      within(everyone).getByText("Everyone in this organization"),
    ).toBeInTheDocument();
  });

  it("keeps a person's direct, team and organization-wide policies apart", async () => {
    await renderAttachments("person:alice");

    expect(
      screen.getByRole("heading", { level: 2, name: "Alice · Person" }),
    ).toBeInTheDocument();
    expect(
      within(section("Attached directly")).getByText(
        "No policy is attached to Alice.",
      ),
    ).toBeInTheDocument();
    const through = section("Through teams");
    expect(
      within(through).getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
    expect(
      within(through).getByRole("link", { name: "Engineering" }),
    ).toBeInTheDocument();
    expect(
      within(section("Organization-wide")).getByRole("link", {
        name: "Baseline read",
      }),
    ).toBeInTheDocument();
    // Presence: the caveat is one sentence and this is a clause of it.
    expect(
      screen.getByText(/this is not a final access decision/),
    ).toBeInTheDocument();
  });

  it("says a team's members were not checked rather than that no team applies", async () => {
    // The control: with the team's members read, its policy reaches
    // Alice through it.
    const allowed = await renderAttachments("person:alice");
    expect(
      within(section("Through teams")).getByRole("link", {
        name: "Deny project deletion",
      }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(http.get(`${API_URL}/orgs/acme/groups/eng/members`, refuse));

    await renderAttachments("person:alice");

    const through = section("Through teams");
    expect(
      within(through).queryByText("No policy reaches Alice through a team."),
    ).not.toBeInTheDocument();
    // Presence: the sentence goes on to name the team and why.
    expect(
      within(through).getByText(/^Not checked: the members of/),
    ).toHaveTextContent("Engineering");
  });

  it("does not call a policy with refused attachments organization-wide", async () => {
    server.use(http.get(`${POLICIES}/0a1b2c3d4e5f6071/attachments`, refuse));

    await renderAttachments("person:alice");

    expect(
      within(section("Organization-wide")).queryByRole("link", {
        name: "Baseline read",
      }),
    ).not.toBeInTheDocument();
    // Presence: the note names the policy inside a sentence.
    expect(screen.getByRole("note")).toHaveTextContent(
      "Not checked: the attachments of Baseline read are not listed to you",
    );
  });

  it("says a subject whose list was refused cannot be shown, not that it is missing", async () => {
    server.use(http.get(`${API_URL}/orgs/acme/members`, refuse));

    await renderAttachments("person:alice");

    expect(
      screen.getByText(
        "This organization's people are not listed to you, so this one cannot be shown.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        "No team, person or agent of this organization matches this address.",
      ),
    ).not.toBeInTheDocument();
  });

  it("addresses subjects by kind and identifier, never by GRN", async () => {
    const { container } = await renderAttachments(alice.grn);

    expect(
      screen.getByText(
        "No team, person or agent of this organization matches this address.",
      ),
    ).toBeInTheDocument();
    const choice = screen.getByLabelText("Team, person or agent");
    const values = within(choice)
      .getAllByRole("option")
      .map((option) => option.getAttribute("value"));
    expect(values).toContain("person:alice");
    expect(values.some((value) => value?.startsWith("grn:"))).toBe(false);
    expect(container.textContent).not.toContain("grn:");
  });
});
