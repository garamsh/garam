import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorsPage from "@/app/console/orgs/[org]/operators/page";
import { TooltipProvider } from "@/components/ui/tooltip";
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

  it("says in words whether garam issued a certificate and shows no ISO date", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          fieldOne,
          {
            ...fieldOne,
            grn: "grn:acme:default:operator:none-one",
            certificateIssuedAt: undefined,
          },
        ]),
      ),
    );

    const { container } = await renderPage();

    const rows = screen.getAllByRole("row").slice(1);
    expect(
      rows.map((row) => within(row).getAllByRole("cell")[2]?.textContent),
    ).toEqual(["Issued", "None"]);
    expect(container.textContent).not.toMatch(/\d{4}-\d{2}-\d{2}/);
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

    await expect(renderPage()).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
