import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PolicyPage from "@/app/console/orgs/[org]/permissions/[policy]/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, baselineRead, denyDeletes } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
}));

const POLICIES = `${API_URL}/orgs/acme/policies`;
const DENY = `${POLICIES}/d1e2f3a4b5c6d7e8`;

function renderPolicy(policy = "d1e2f3a4b5c6d7e8") {
  return renderServerComponent(
    PolicyPage({ params: Promise.resolve({ org: "acme", policy }) }),
  );
}

describe("policy page", () => {
  it("shows the state, whom it is attached to by name, and each version", async () => {
    const { container } = await renderPolicy();

    expect(
      screen.getByRole("heading", { level: 1, name: "Deny project deletion" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Applies to Engineering (Team)."),
    ).toBeInTheDocument();
    // Presence: a row's accessible name is all of its cells.
    const row = screen.getByRole("row", { name: /Engineering/ });
    expect(within(row).getByText("Team")).toBeInTheDocument();
    const current = screen.getByRole("listitem", { name: "Version 1" });
    expect(within(current).getByText("Default")).toBeInTheDocument();
    const newer = screen.getByRole("listitem", { name: "Version 2" });
    expect(
      within(newer).getByRole("button", { name: "Make default" }),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:acme:default:group");
  });

  it("says a draft grants nothing, and whom it would reach once active", async () => {
    server.use(
      http.get(POLICIES, () =>
        HttpResponse.json([{ ...denyDeletes, status: "draft" }]),
      ),
      http.get(`${DENY}/attachments`, () => HttpResponse.json([])),
    );

    await renderPolicy();

    expect(
      screen.getByText("Draft: grants no access until it is activated."),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Not evaluated while draft. Once active: No attachments: applies to everyone in this organization.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Activate…" }),
    ).toBeInTheDocument();
  });

  it("warns that attaching narrows an active policy that reaches everyone", async () => {
    server.use(http.get(POLICIES, () => HttpResponse.json([baselineRead])));

    await renderPolicy("0a1b2c3d4e5f6071");

    expect(
      screen.getByText(
        "No attachments: applies to everyone in this organization.",
      ),
    ).toBeInTheDocument();
    // Presence: the warning is a sentence that goes on to say how.
    expect(screen.getByText(/^Attaching narrows it/)).toBeInTheDocument();
  });

  it("does not call refused attachments no attachments", async () => {
    // The control: listed as empty, the policy says it has none.
    server.use(http.get(`${DENY}/attachments`, () => HttpResponse.json([])));
    const listed = await renderPolicy();
    expect(screen.getByText(/^No attachments\./)).toBeInTheDocument();
    listed.unmount();

    server.use(
      http.get(`${DENY}/attachments`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 }),
      ),
    );

    await renderPolicy();

    expect(
      screen.getByText(
        "garam refused to list this policy's attachments to you.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/^No attachments\./)).not.toBeInTheDocument();
    // Presence: the reach is a sentence that goes on to say why.
    expect(
      screen.getByText(/may apply to everyone in this organization\.$/),
    ).toBeInTheDocument();
  });

  it("renders not-found for a policy the organization's list does not hold", async () => {
    // garam answers an empty list of versions and attachments for any
    // identifier, so only the policy list can say it does not exist.
    server.use(
      http.get(`${POLICIES}/nope/versions`, () => HttpResponse.json([])),
      http.get(`${POLICIES}/nope/attachments`, () => HttpResponse.json([])),
    );

    await expect(renderPolicy("nope")).rejects.toThrow("NEXT_NOT_FOUND");
  });
});
