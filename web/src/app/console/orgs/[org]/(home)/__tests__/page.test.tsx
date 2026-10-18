import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import HomePage from "@/app/console/orgs/[org]/(home)/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  alice,
  fieldOne,
  reportedAgent,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `notFound()` and `redirect()` throw control-flow errors the router
// catches; the tests only need to observe which one was reached.
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

function refuse(path: string) {
  return http.get(`${API_URL}/orgs/acme/${path}`, () =>
    HttpResponse.json(
      { kind: "permission_denied", message: "no" },
      { status: 403 },
    ),
  );
}

/** The page's lede, which is the paragraph under its title. */
function lede(): HTMLElement {
  const title = screen.getByRole("heading", { level: 1 });
  const paragraph = title.parentElement?.querySelector("p");
  if (!paragraph) throw new Error("no lede under the title");
  return paragraph;
}

describe("Home", () => {
  it("names the organization and prints no GRN", async () => {
    const { container } = await renderServerComponent(HomePage(params("acme")));

    expect(
      screen.getByRole("heading", { level: 1, name: "Acme" }),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("opens with a sentence linking the caller's teams and what needs a look", async () => {
    await renderServerComponent(HomePage(params("acme")));

    expect(lede()).toHaveTextContent(
      "Nothing is waiting on you. You are in Engineering. Unnamed agent needs a look.",
    );
    expect(
      within(lede()).getByRole("link", { name: "Engineering" }),
    ).toHaveAttribute("href", "/console/orgs/acme/teams/eng");
    expect(
      within(lede()).getByRole("link", { name: "Unnamed agent" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents/1c3d5e7f90a2b4c6");
  });

  it("says nothing waits on the caller, since garam has no work items", async () => {
    await renderServerComponent(HomePage(params("acme")));

    const needs = screen.getByRole("region", { name: /^Needs you/ });
    expect(needs).toHaveTextContent(/garam has no work items yet/);
  });

  it("lists the caller's teams with the people and agents in each", async () => {
    await renderServerComponent(HomePage(params("acme")));

    const teams = screen.getByRole("region", { name: /^Your teams/ });
    expect(
      within(teams).getByRole("link", { name: /Engineering/ }),
    ).toHaveAttribute("href", "/console/orgs/acme/teams/eng");
    // What each row says, the decorative mark aside.
    const members = within(teams)
      .getAllByRole("listitem")
      .filter((item) => item.closest("ul")?.closest("li") !== null)
      .map((item) =>
        [...item.children]
          .filter((child) => child.getAttribute("aria-hidden") !== "true")
          .map((child) => child.textContent)
          .join(" "),
      );
    expect(members).toEqual([`${alice.nickname} you`, "Unnamed agent agent"]);
  });

  it("lists only the teams garam answers the caller is in", async () => {
    let asked = "";
    server.use(
      http.get(`${API_URL}/orgs/acme/groups`, ({ request }) => {
        asked = new URL(request.url).search;
        return HttpResponse.json([]);
      }),
    );

    await renderServerComponent(HomePage(params("acme")));

    // One request names the caller's own teams (#1562).
    expect(asked).toBe("?member=me");
    const teams = screen.getByRole("region", { name: /^Your teams/ });
    expect(teams).toHaveTextContent("You are in no team in this organization.");
    expect(lede()).toHaveTextContent("You are in no team here.");
  });

  it("keeps a team whose members it could not read, saying so", async () => {
    server.use(refuse("groups/eng/members"));

    await renderServerComponent(HomePage(params("acme")));

    const teams = screen.getByRole("region", { name: /^Your teams/ });
    expect(
      within(teams).getByRole("link", { name: /Engineering/ }),
    ).toBeInTheDocument();
    expect(teams).toHaveTextContent("Its members were not shown to you.");
  });

  it("lists what the caller can read that is failing, each linked", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([{ ...fieldOne, enrollmentState: "expired" }]),
      ),
    );

    await renderServerComponent(HomePage(params("acme")));

    const look = screen.getByRole("region", { name: /^Worth a look/ });
    expect(
      within(look).getByRole("link", { name: "Unnamed agent" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents/1c3d5e7f90a2b4c6");
    expect(look).toHaveTextContent(/Provisioning failed.*Disconnected/);
    expect(
      within(look).getByRole("link", { name: fieldOne.name }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(look).toHaveTextContent("Enrollment expired");
  });

  it("says nothing it can read is failing when nothing is", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/agents`, () =>
        HttpResponse.json([
          {
            ...reportedAgent,
            provisioningState: "ready",
            runtimeState: "connected",
          },
        ]),
      ),
    );

    await renderServerComponent(HomePage(params("acme")));

    const look = screen.getByRole("region", { name: /^Worth a look/ });
    expect(look).toHaveTextContent("Nothing you can read is failing.");
    expect(lede()).toHaveTextContent("Nothing you can read is failing.");
  });

  it("says which list it left out of Worth a look", async () => {
    server.use(refuse("operators"));

    await renderServerComponent(HomePage(params("acme")));

    const look = screen.getByRole("region", { name: /^Worth a look/ });
    expect(look).toHaveTextContent(
      "Operators are not included: you can't view them.",
    );
    // Control: the agents it could read are still listed.
    expect(
      within(look).getByRole("link", { name: "Unnamed agent" }),
    ).toBeInTheDocument();
  });

  it("claims nothing for a reader who can see little", async () => {
    server.use(refuse("groups"), refuse("agents"), refuse("operators"));

    await renderServerComponent(HomePage(params("acme")));

    expect(lede()).toHaveTextContent(/^Nothing is waiting on you\.$/);
    expect(
      within(screen.getByRole("region", { name: /^Your teams/ })).getByText(
        "You can't view this organization's teams",
      ),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: /^Worth a look/ })).getByText(
        "You can't view this organization's agents or operators",
      ),
    ).toBeInTheDocument();
  });

  it("answers not found for an organization the caller cannot read", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme`, () =>
        HttpResponse.json({ message: "no" }, { status: 403 }),
      ),
    );

    await expect(
      renderServerComponent(HomePage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API did not answer rather than showing an empty Home", async () => {
    server.use(http.get(`${API_URL}/orgs/acme`, () => HttpResponse.error()));

    await renderServerComponent(HomePage(params("acme")));

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: /^Your teams/ })).toBeNull();
  });
});
