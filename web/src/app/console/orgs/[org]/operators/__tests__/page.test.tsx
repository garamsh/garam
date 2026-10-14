import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorsPage from "@/app/console/orgs/[org]/operators/page";
import {
  API_URL,
  answerPermissionChecks,
  fieldOne,
  signedOut,
} from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
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

describe("operators section", () => {
  it("lists an operator by name, linking to its page, with its identifier beneath", async () => {
    const { container } = await renderServerComponent(
      OperatorsPage(params("acme")),
    );

    expect(
      screen.getByRole("heading", { name: "Operators" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(screen.getByText("field-one")).toBeInTheDocument();
    // ADR-0011: the console addresses a resource by the GRN's last
    // segment and prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("tells two operators of one name apart by identifier", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          fieldOne,
          { ...fieldOne, grn: "grn:acme:default:operator:field-two" },
        ]),
      ),
    );

    await renderServerComponent(OperatorsPage(params("acme")));

    const links = screen.getAllByRole("link", { name: "Field cluster one" });
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "/console/orgs/acme/operators/field-one",
      "/console/orgs/acme/operators/field-two",
    ]);
    expect(screen.getByText("field-one")).toBeInTheDocument();
    expect(screen.getByText("field-two")).toBeInTheDocument();
  });

  it("tells the three enrollment states apart and never calls one live", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          fieldOne,
          {
            ...fieldOne,
            grn: "grn:acme:default:operator:lapsed",
            enrollmentState: "expired",
            certificateIssuedAt: undefined,
          },
          {
            ...fieldOne,
            grn: "grn:acme:default:operator:waiting",
            enrollmentState: "pending",
            certificateIssuedAt: undefined,
          },
        ]),
      ),
    );

    await renderServerComponent(OperatorsPage(params("acme")));

    // Three states, three words, under a column that says enrollment
    // rather than status.
    expect(
      screen.getByRole("columnheader", { name: "Enrollment" }),
    ).toBeInTheDocument();
    expect(screen.getByText("enrolled")).toBeInTheDocument();
    expect(screen.getByText("expired")).toBeInTheDocument();
    expect(screen.getByText("pending")).toBeInTheDocument();
    expect(
      screen.getByText("Enrollment is not a sign an operator is running."),
    ).toBeInTheDocument();
  });

  it("registers under the list, and deletes from none of the rows", async () => {
    await renderServerComponent(OperatorsPage(params("acme")));

    // Registering has no route of its own: `new` is a valid identifier,
    // so that segment is an operator's page (ADR-0074).
    expect(screen.getByLabelText("Identifier")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Register operator" }),
    ).not.toBeInTheDocument();
    const table = screen.getByRole("table");
    expect(
      within(table).queryByRole("button", { name: /Delete/ }),
    ).not.toBeInTheDocument();
  });

  it("leaves the register form out for a caller the server denies registering", async () => {
    // The control: an owner, whom the server allows, is offered it.
    const owner = await renderServerComponent(OperatorsPage(params("acme")));
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeInTheDocument();
    owner.unmount();

    server.use(
      answerPermissionChecks((action) =>
        action === "operator:register" ? "deny" : "allow",
      ),
    );
    await renderServerComponent(OperatorsPage(params("acme")));

    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Identifier")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Register operator" }),
    ).not.toBeInTheDocument();
  });

  it("offers the register form when the check could not be asked", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/permission-checks`, () =>
        HttpResponse.error(),
      ),
    );

    await renderServerComponent(OperatorsPage(params("acme")));

    // The list still reads, and the server is left to refuse the form.
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeInTheDocument();
  });

  it("renders its list and the register form when the check answers outside its contract", async () => {
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});
    server.use(
      http.post(`${API_URL}/orgs/acme/permission-checks`, () =>
        HttpResponse.json({ results: [] }),
      ),
    );

    await renderServerComponent(OperatorsPage(params("acme")));

    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeInTheDocument();
    logged.mockRestore();
  });

  it("shows an empty state rather than a bare list", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () => HttpResponse.json([])),
    );

    await renderServerComponent(OperatorsPage(params("acme")));

    expect(
      screen.getByText("This organization has registered no operators."),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Identifier")).toBeInTheDocument();
  });

  it.each([403, 404])("renders not-found on %i", async (status) => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json({ kind: "denied", message: "no" }, { status }),
      ),
    );

    await expect(
      renderServerComponent(OperatorsPage(params("acme"))),
    ).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than reporting no operators", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(OperatorsPage(params("acme")));

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This organization has registered no operators."),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(OperatorsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
