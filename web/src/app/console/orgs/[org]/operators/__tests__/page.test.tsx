import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorsPage from "@/app/console/orgs/[org]/operators/page";
import { API_URL, fieldOne, signedOut } from "@/testing/mocks/handlers";
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
  it("lists an operator by identifier and who registered it", async () => {
    const { container } = await renderServerComponent(
      OperatorsPage(params("acme")),
    );

    expect(
      screen.getByRole("heading", { name: "Operators" }),
    ).toBeInTheDocument();
    expect(screen.getByText("field-one")).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    // ADR-0011: the console addresses a resource by the GRN's last
    // segment and prints no GRN.
    expect(container.textContent).not.toContain("grn:");
  });

  it("says whether an operator enrolled and when garam last signed for it", async () => {
    await renderServerComponent(OperatorsPage(params("acme")));

    expect(screen.getByText("enrolled")).toBeInTheDocument();
    expect(screen.getByText("2026-03-04")).toBeInTheDocument();
    // The registration date is the control: both are rendered, so the
    // certificate's date is a fact of its own rather than the record's
    // under another label.
    expect(screen.getByText("2026-01-02")).toBeInTheDocument();
    expect(screen.getByText("Certificate")).toBeInTheDocument();
  });

  it("tells a token that ran out from one still outstanding", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
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

    // Three states, three words. A list rendering one word for two of
    // them is the defect the state exists to remove.
    expect(screen.getByText("expired")).toBeInTheDocument();
    expect(screen.getByText("pending")).toBeInTheDocument();
    expect(screen.queryByText("enrolled")).not.toBeInTheDocument();
    // Neither holds a certificate, so neither shows the row that says
    // when garam signed one.
    expect(screen.queryByText("Certificate")).not.toBeInTheDocument();
  });

  it("shows an empty state rather than a bare list", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () => HttpResponse.json([])),
    );

    await renderServerComponent(OperatorsPage(params("acme")));

    expect(
      screen.getByText("This organization has registered no operators."),
    ).toBeInTheDocument();
  });

  it("offers the register form to anyone who may read the section", async () => {
    await renderServerComponent(OperatorsPage(params("acme")));

    expect(screen.getByLabelText("Identifier")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Register operator" }),
    ).toBeInTheDocument();
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
