import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorPage from "@/app/console/orgs/[org]/operators/[operator]/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  fieldAgent,
  fieldOne,
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
  usePathname: () => "/console/orgs/acme/operators/field-one",
}));

const OPERATOR = `${API_URL}/orgs/acme/operators/field-one`;
const DEFINITIONS = `${API_URL}/orgs/acme/agent-definitions`;

function renderOperator() {
  return renderServerComponent(
    OperatorPage({
      params: Promise.resolve({ org: "acme", operator: "field-one" }),
    }),
  );
}

describe("operator page", () => {
  it("shows the operator by name with its description and identifier, and prints no GRN", async () => {
    const { container } = await renderOperator();

    expect(
      screen.getByRole("heading", { level: 1, name: "Field cluster one" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Runs the batch agents.", { selector: "p" }),
    ).toBeInTheDocument();
    expect(screen.getByText("field-one")).toBeInTheDocument();
    // ADR-0011: the identifier is shown and the GRN is not.
    expect(container.textContent).not.toContain("grn:");
  });

  it("says what enrollment holds and that runtime is unknown", async () => {
    await renderOperator();

    expect(screen.getByText("enrolled")).toBeInTheDocument();
    expect(screen.getByText("Certificate issued")).toBeInTheDocument();
    expect(screen.getByText("Last certificate")).toBeInTheDocument();
    expect(screen.getByText("2026-03-04")).toBeInTheDocument();
    expect(screen.getByText("Unknown — no report")).toBeInTheDocument();
  });

  it("says a pending operator holds a token without claiming when it runs out", async () => {
    server.use(
      http.get(OPERATOR, () =>
        HttpResponse.json({
          ...fieldOne,
          enrollmentState: "pending",
          certificateIssuedAt: undefined,
        }),
      ),
    );

    await renderOperator();

    expect(screen.getByText("Enrollment token available")).toBeInTheDocument();
    // The control: the row only an issued certificate carries is gone,
    // so the label above is the state's rather than the page's.
    expect(screen.queryByText("Last certificate")).not.toBeInTheDocument();
    expect(screen.getByText("Unknown — no report")).toBeInTheDocument();
  });

  it("lists the definitions naming it, says they refuse the delete, and links to them", async () => {
    server.use(
      http.get(DEFINITIONS, () =>
        HttpResponse.json([
          fieldAgent,
          {
            ...fieldAgent,
            agentGrn: "grn:acme:default:agent:0000000000000001",
            operatorGrn: "grn:acme:default:operator:elsewhere",
          },
        ]),
      ),
    );

    await renderOperator();

    const section = screen
      .getByRole("heading", { name: "Agent definitions" })
      .closest("section");
    expect(section).not.toBeNull();
    const definitions = within(section as HTMLElement);
    expect(definitions.getByText("9f2ac1b40d8e7a35")).toBeInTheDocument();
    // A definition naming another operator is not this one's.
    expect(
      definitions.queryByText("0000000000000001"),
    ).not.toBeInTheDocument();
    expect(
      definitions.getByText(
        "Deleting is refused while this definition names this operator.",
      ),
    ).toBeInTheDocument();
    expect(
      definitions.getByRole("link", { name: "View in Agents" }),
    ).toHaveAttribute("href", "/console/orgs/acme/agents#definitions");
  });

  it("says no definition names it when none does", async () => {
    server.use(http.get(DEFINITIONS, () => HttpResponse.json([])));

    await renderOperator();

    expect(
      screen.getByText("No agent definition names this operator."),
    ).toBeInTheDocument();
  });

  it("says the definitions are unchecked rather than none when they are refused", async () => {
    server.use(
      http.get(DEFINITIONS, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderOperator();

    expect(
      screen.getByText("Not checked: agent definitions are not listed to you."),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("No agent definition names this operator."),
    ).not.toBeInTheDocument();
    // The rest of the page stands.
    expect(
      screen.getByRole("heading", { level: 1, name: "Field cluster one" }),
    ).toBeInTheDocument();
  });

  it("offers editing the details and deleting the registration", async () => {
    await renderOperator();

    expect(screen.getByLabelText("Name")).toHaveValue("Field cluster one");
    expect(screen.getByLabelText("Description")).toHaveValue(
      "Runs the batch agents.",
    );
    expect(
      screen.getByRole("button", { name: "Delete operator" }),
    ).toBeInTheDocument();
  });

  it("says the operator is refused rather than missing when garam answers 403", async () => {
    server.use(
      http.get(OPERATOR, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderOperator();

    expect(
      screen.getByRole("heading", { name: "You cannot view this operator" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Name")).not.toBeInTheDocument();
  });

  it("renders not-found for an operator that is not registered", async () => {
    server.use(
      http.get(OPERATOR, () =>
        HttpResponse.json(
          { kind: "not_found", message: "operator not found" },
          { status: 404 },
        ),
      ),
    );

    await expect(renderOperator()).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than showing an empty operator", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderOperator();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
  });

  it("sends a rejected session to the login page", async () => {
    server.use(...signedOut());

    await expect(renderOperator()).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
