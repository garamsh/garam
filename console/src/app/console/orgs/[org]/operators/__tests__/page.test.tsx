import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorsPage from "@/app/console/orgs/[org]/operators/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
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

function params(org: string, connect?: string) {
  return {
    params: Promise.resolve({ org }),
    searchParams: Promise.resolve(connect === undefined ? {} : { connect }),
  };
}

function renderPage(org = "acme", connect?: string) {
  return renderServerComponent(OperatorsPage(params(org, connect)), {
    wrapper: TooltipProvider,
  });
}

describe("operators list", () => {
  it("lists an operator by name, linking to its page, with its identifier beneath", async () => {
    const { container } = await renderPage();

    expect(
      screen.getByRole("heading", { level: 1, name: /Operators/ }),
    ).toHaveTextContent("Operators1");
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(screen.getByText("field-one")).toBeInTheDocument();
    // ADR-0011: no GRN is printed.
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

    await renderPage();

    const links = screen.getAllByRole("link", { name: "Field cluster one" });
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "/console/orgs/acme/operators/field-one",
      "/console/orgs/acme/operators/field-two",
    ]);
  });

  it("names the control service's host where one is registered", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          { ...fieldOne, controlEndpoint: "https://control.example.com/v1" },
        ]),
      ),
    );

    await renderPage();

    expect(screen.getByText("control.example.com")).toBeInTheDocument();
  });

  // The pair is read from the control service's `manager` (ADR-0124),
  // never from names: `field-one-control` below is the name a person
  // would pair by, and only the record joins it (#1636).
  const control = {
    ...fieldOne,
    grn: "grn:acme:default:operator:field-one-control",
    name: "Field one control",
    controlEndpoint: "https://control.example.com",
  };

  function listing(...operators: unknown[]) {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(operators),
      ),
    );
  }

  it("shows a linked pair as one Operator: its cluster manager and its control service", async () => {
    listing(fieldOne, { ...control, manager: fieldOne.grn });

    await renderPage();

    expect(
      screen.getAllByRole("columnheader").map((header) => header.textContent),
    ).toEqual(expect.arrayContaining(["Cluster manager", "Control service"]));

    expect(
      screen.getByRole("heading", { level: 1, name: /Operators/ }),
    ).toHaveTextContent("Operators1");
    const [, row] = screen.getAllByRole("row");
    expect(
      within(row).getByRole("link", { name: "Field cluster one" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one");
    expect(
      within(row).getByRole("link", { name: "Field one control" }),
    ).toHaveAttribute("href", "/console/orgs/acme/operators/field-one-control");
    expect(within(row).getByText("control.example.com")).toBeInTheDocument();
    expect(within(row).queryByText("Not connected")).toBeNull();
  });

  it("says a cluster manager with no control service is not connected, and pairs nothing by name", async () => {
    // The same two records with no `manager`: two Operators, neither
    // connected, though one is named like the other's control service.
    listing(fieldOne, control);

    await renderPage();

    expect(
      screen.getByRole("heading", { level: 1, name: /Operators/ }),
    ).toHaveTextContent("Operators2");
    expect(screen.getAllByText("Not connected")).toHaveLength(2);
  });

  it("shows a lone control service under the cluster manager it names, which garam no longer holds", async () => {
    listing({ ...control, manager: fieldOne.grn });

    await renderPage();

    const [, row] = screen.getAllByRole("row");
    expect(within(row).getByText("Not registered")).toBeInTheDocument();
    expect(within(row).getByText("field-one")).toBeInTheDocument();
    expect(
      within(row).getByRole("link", { name: "Field one control" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Field cluster one" }),
    ).toBeNull();
  });

  it("offers only cluster managers without a control service to connect one to, choosing the one the address names", async () => {
    const fieldTwo = {
      ...fieldOne,
      grn: "grn:acme:default:operator:field-two",
      name: "Field cluster two",
    };
    listing(fieldOne, { ...control, manager: fieldOne.grn }, fieldTwo);
    const user = userEvent.setup();

    await renderPage("acme", "field-two");

    const identity = screen.getByRole("combobox", { name: "Register" });
    expect(identity).toHaveTextContent(
      "The control service of Field cluster two",
    );
    await user.click(identity);
    expect(
      (await screen.findAllByRole("option")).map(
        (option) => option.textContent,
      ),
    ).toEqual([
      "A new Operator",
      "The control service of Field cluster two field-two",
    ]);
  });

  it("offers connecting an operator to a caller the server allows", async () => {
    await renderPage();

    expect(
      screen.getByRole("button", { name: "Connect an operator" }),
    ).toBeInTheDocument();
  });

  it("leaves connecting out for a caller the server denies registering", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:register" ? "deny" : "allow",
      ),
    );

    await renderPage();

    expect(
      screen.queryByRole("button", { name: "Connect an operator" }),
    ).not.toBeInTheDocument();
    // Control: the list itself is still there.
    expect(
      screen.getByRole("link", { name: "Field cluster one" }),
    ).toBeInTheDocument();
  });

  it("offers connecting when the server cannot decide", async () => {
    server.use(answerPermissionChecks(() => "unknown"));

    await renderPage();

    expect(
      screen.getByRole("button", { name: "Connect an operator" }),
    ).toBeInTheDocument();
  });

  it("opens the connect dialog when the address asks for it", async () => {
    await renderPage("acme", "");

    expect(
      screen.getByRole("dialog", { name: "Connect an operator" }),
    ).toBeInTheDocument();
  });

  it("names the absence when no operator is registered", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () => HttpResponse.json([])),
    );

    await renderPage();

    expect(screen.getByText("No operators registered")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("says the list was refused rather than empty when garam answers 403", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "denied" },
          { status: 403 },
        ),
      ),
    );

    await renderPage();

    expect(screen.getByRole("status")).toHaveTextContent(
      "You can't view this organization's operators",
    );
    expect(
      screen.queryByText("No operators registered"),
    ).not.toBeInTheDocument();
  });

  it("says the API is unreachable rather than reporting no operators", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () => HttpResponse.error()),
    );

    await renderPage();

    expect(screen.getByRole("status")).toHaveTextContent(
      "Cannot reach the API",
    );
    expect(
      screen.queryByText("No operators registered"),
    ).not.toBeInTheDocument();
  });

  it("sends a rejected session to the login page, not to the notice", async () => {
    server.use(...signedOut());

    await expect(renderPage()).rejects.toThrow(
      "NEXT_REDIRECT:http://localhost:3001/login",
    );
  });
});
