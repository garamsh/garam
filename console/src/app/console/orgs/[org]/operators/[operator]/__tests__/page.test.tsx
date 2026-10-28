import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OperatorPage from "@/app/console/orgs/[org]/operators/[operator]/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
  fieldOne,
  fieldOneKey,
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
  useRouter: () => ({ refresh: () => {} }),
}));

const OPERATOR = `${API_URL}/orgs/acme/operators/field-one`;

function renderOperator(tab?: string) {
  return renderServerComponent(
    OperatorPage({
      params: Promise.resolve({ org: "acme", operator: "field-one" }),
      searchParams: Promise.resolve(tab ? { tab } : {}),
    }),
    { wrapper: TooltipProvider },
  );
}

/** The value a fact of the overview's description list holds. */
function fact(term: string) {
  const dt = screen.getByText(term, { selector: "dt" });
  return dt.nextElementSibling as HTMLElement;
}

describe("operator page", () => {
  it("titles the page with its name, offers its identifier to copy, and prints no GRN or ISO date", async () => {
    const { container } = await renderOperator();

    expect(
      screen.getByRole("heading", { level: 1, name: "Field cluster one" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      "field-one",
    );
    expect(container.textContent).not.toContain("grn:");
    expect(container.textContent).not.toMatch(/\d{4}-\d{2}-\d{2}T/);
  });

  it("opens on Overview with the five tabs the area offers", async () => {
    await renderOperator();

    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual([
      "Overview",
      "Keys",
      "Control service",
      "Delegation",
      "Settings",
    ]);
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("says what garam knows of it", async () => {
    await renderOperator();

    expect(fact("Description")).toHaveTextContent("Runs the batch agents.");
    expect(fact("Signs in with")).toHaveTextContent("Key in use");
    expect(screen.queryByText("Certificate", { selector: "dt" })).toBeNull();
    // Registered by a member, named rather than identified.
    expect(fact("Registered")).toHaveTextContent("by Alice");
  });

  it("lists its keys with their state and last sign-in, and offers rotating and revoking", async () => {
    await renderOperator("keys");

    const table = within(screen.getByRole("table", { name: "Keys" }));
    const row = table.getAllByRole("row")[1];
    expect(row).toHaveTextContent(fieldOneKey.kid.slice(0, 12));
    expect(row).toHaveTextContent("Active");
    expect(
      within(row).getByRole("button", { name: "Revoke" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Rotate key" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/operators/field-one/connect?rotate",
    );
    expect(
      screen.queryByRole("link", { name: "Finish setup" }),
    ).not.toBeInTheDocument();
  });

  it("sends an operator without a key to its setup", async () => {
    server.use(
      http.get(`${OPERATOR}/keys`, () => HttpResponse.json({ keys: [] })),
    );

    await renderOperator("keys");

    expect(screen.getByRole("link", { name: "Finish setup" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/operators/field-one/connect",
    );
    expect(
      screen.getByRole("link", { name: "Issue a key" }),
    ).toBeInTheDocument();
    expect(screen.getByText("No keys issued")).toBeInTheDocument();
  });

  it("says its keys were refused rather than none", async () => {
    server.use(
      http.get(`${OPERATOR}/keys`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderOperator("keys");

    expect(
      screen.getByText("You can't view this operator's keys"),
    ).toBeInTheDocument();
    expect(screen.queryByText("No keys issued")).not.toBeInTheDocument();
  });

  it("leaves out rotating and revoking where the server denies re-issuing", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:reissue" ? "deny" : "allow",
      ),
    );

    await renderOperator("keys");

    expect(
      screen.queryByRole("link", { name: "Rotate key" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Revoke" }),
    ).not.toBeInTheDocument();
    // Control: the keys themselves are still listed.
    expect(screen.getByRole("table", { name: "Keys" })).toBeInTheDocument();
  });

  it("leaves out replacing and withdrawing where the server denies delegating", async () => {
    server.use(
      http.get(`${OPERATOR}/delegation`, () =>
        HttpResponse.json({
          grantId: "g-1",
          operatorGrn: fieldOne.grn,
          controllers: [fieldOne.grn],
          operations: ["agent:create"],
          expiresAt: "2026-10-21T00:00:00Z",
          grantedBy: "grn:root:default:user:alice",
          grantedAt: "2026-10-14T00:00:00Z",
        }),
      ),
      answerPermissionChecks((action) =>
        action === "operator:delegate" ? "deny" : "allow",
      ),
    );

    await renderOperator("delegation");

    expect(
      screen.queryByRole("button", { name: "Replace delegation" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Withdraw delegation" }),
    ).not.toBeInTheDocument();
    // Control: the grant itself is still read out.
    expect(fact("Operations")).toHaveTextContent(
      "Create agents from published templates",
    );
  });

  it("offers replacing and withdrawing where the server allows delegating", async () => {
    server.use(
      http.get(`${OPERATOR}/delegation`, () =>
        HttpResponse.json({
          grantId: "g-1",
          operatorGrn: fieldOne.grn,
          controllers: [fieldOne.grn],
          operations: ["agent:create"],
          expiresAt: "2026-10-21T00:00:00Z",
          grantedBy: "grn:root:default:user:alice",
          grantedAt: "2026-10-14T00:00:00Z",
        }),
      ),
    );

    await renderOperator("delegation");

    expect(
      screen.getByRole("button", { name: "Replace delegation" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Withdraw delegation" }),
    ).toBeInTheDocument();
  });

  it("offers a first key to a caller garam allows re-issuing alone", async () => {
    server.use(
      http.get(`${OPERATOR}/keys`, () => HttpResponse.json({ keys: [] })),
      answerPermissionChecks((action) =>
        action === "operator:register" ? "deny" : "allow",
      ),
    );

    await renderOperator("keys");

    expect(
      screen.getByRole("link", { name: "Issue a key" }),
    ).toBeInTheDocument();
  });

  it("offers no first key where the server denies registering and re-issuing", async () => {
    server.use(
      http.get(`${OPERATOR}/keys`, () => HttpResponse.json({ keys: [] })),
      answerPermissionChecks((action) =>
        action === "operator:register" || action === "operator:reissue"
          ? "deny"
          : "allow",
      ),
    );

    await renderOperator("keys");

    expect(
      screen.queryByRole("link", { name: "Issue a key" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Finish setup" }),
    ).not.toBeInTheDocument();
  });

  it("offers every action when the server cannot decide", async () => {
    server.use(answerPermissionChecks(() => "unknown"));

    await renderOperator("settings");

    expect(screen.getByRole("textbox", { name: "Name" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Delete operator" }),
    ).toBeInTheDocument();
    // An operator enters by a key alone, so Settings offers no legacy
    // credential and no enrollment token however much is allowed
    // (ADR-0117).
    expect(
      screen.queryByRole("button", { name: "Re-issue credential" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Issue a new enrollment token" }),
    ).not.toBeInTheDocument();
  });

  it("closes Settings with a danger zone, which a denied delete leaves out", async () => {
    await renderOperator("settings");
    expect(
      screen.getByRole("region", { name: "Danger zone" }),
    ).toBeInTheDocument();
  });

  it("leaves the danger zone out where the server denies deleting", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:delete" ? "deny" : "allow",
      ),
    );

    await renderOperator("settings");

    expect(
      screen.queryByRole("region", { name: "Danger zone" }),
    ).not.toBeInTheDocument();
    // Control: the rest of Settings stays.
    expect(screen.getByRole("textbox", { name: "Name" })).toBeInTheDocument();
  });

  it("names a delegation's controllers rather than printing their GRNs", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/operators`, () =>
        HttpResponse.json([
          fieldOne,
          {
            ...fieldOne,
            grn: "grn:acme:default:operator:field-manager",
            name: "Field manager",
          },
        ]),
      ),
      http.get(`${OPERATOR}/delegation`, () =>
        HttpResponse.json({
          grantId: "g-1",
          operatorGrn: fieldOne.grn,
          controllers: ["grn:acme:default:operator:field-manager"],
          operations: ["agent:create"],
          expiresAt: "2026-10-21T00:00:00Z",
          grantedBy: "grn:root:default:user:alice",
          grantedAt: "2026-10-14T00:00:00Z",
        }),
      ),
    );

    const { container } = await renderOperator("delegation");

    expect(fact("Controllers")).toHaveTextContent("Field manager");
    expect(fact("Operations")).toHaveTextContent(
      "Create agents from published templates",
    );
    expect(container.textContent).not.toContain("grn:");
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

    expect(screen.getByRole("status")).toHaveTextContent(
      "You can't view this operator",
    );
  });

  it("renders not-found for an operator that is not registered", async () => {
    server.use(
      http.get(OPERATOR, () =>
        HttpResponse.json(
          { kind: "not_found", message: "no" },
          { status: 404 },
        ),
      ),
    );

    await expect(renderOperator()).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than showing an empty operator", async () => {
    server.use(http.get(OPERATOR, () => HttpResponse.error()));

    await renderOperator();

    expect(screen.getByRole("status")).toHaveTextContent(
      "Cannot reach the API",
    );
  });

  it("sends a rejected session to the login page", async () => {
    server.use(...signedOut());

    await expect(renderOperator()).rejects.toThrow(
      "NEXT_REDIRECT:http://garam.localhost:3001/login",
    );
  });
});
