import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ConnectOperatorPage from "@/app/console/orgs/[org]/operators/[operator]/connect/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import {
  API_URL,
  answerPermissionChecks,
  fieldOne,
  fieldOneKey,
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
  usePathname: () => "/console/orgs/acme/operators/field-one/connect",
  useRouter: () => ({ refresh: () => {} }),
}));

const KEYS = `${API_URL}/orgs/acme/operators/field-one/keys`;

function keysAre(...keys: unknown[]) {
  server.use(http.get(KEYS, () => HttpResponse.json({ keys })));
}

function renderPage(search: { rotate?: string; pair?: string } = {}) {
  return renderServerComponent(
    ConnectOperatorPage({
      params: Promise.resolve({ org: "acme", operator: "field-one" }),
      searchParams: Promise.resolve(search),
    }),
    { wrapper: TooltipProvider },
  );
}

function currentStep() {
  return within(screen.getByRole("list", { name: /steps$/ }))
    .getAllByRole("listitem")
    .find((item) => item.getAttribute("aria-current") === "step");
}

const CONTROL = {
  ...fieldOne,
  grn: "grn:acme:default:operator:field-one-control",
  name: "field-one-control",
  manager: fieldOne.grn,
};

/** Lists field-one beside a control service whose record names it. */
function pairedWithControlService(controlKeys: unknown[] = []) {
  server.use(
    http.get(`${API_URL}/orgs/acme/operators/field-one-control`, () =>
      HttpResponse.json(CONTROL),
    ),
    http.get(`${API_URL}/orgs/acme/operators`, () =>
      HttpResponse.json([fieldOne, CONTROL]),
    ),
    http.get(`${API_URL}/orgs/acme/operators/field-one-control/keys`, () =>
      HttpResponse.json({ keys: controlKeys }),
    ),
  );
}

describe("connect operator page", () => {
  it("sets up a cluster manager and its control service as one Operator, issuing the key still missing (#1636)", async () => {
    pairedWithControlService();

    await renderPage({ pair: "field-one-control" });

    expect(currentStep()).toHaveTextContent("Issue the keys");
    expect(
      screen.getByRole("button", { name: "Issue the control service's key" }),
    ).toBeInTheDocument();
  });

  it("replaces one identity's key on its own when asked to rotate, though it has a control service", async () => {
    // The control for the test above: the same pair, with `?rotate`.
    pairedWithControlService();

    await renderPage({ rotate: "", pair: "field-one-control" });

    expect(currentStep()).toHaveTextContent("Issue a key");
  });

  it("reads a pair from its address alone, so a listing never swaps the wizard", async () => {
    // The listing pairs field-one, and the address names no pair.
    pairedWithControlService();
    keysAre();

    await renderPage();

    expect(
      screen.getByRole("button", { name: "Issue key" }),
    ).toBeInTheDocument();
  });

  it("keeps the pair's wizard when a read fails, so issuing's re-render keeps its keys", async () => {
    pairedWithControlService();
    server.use(
      http.get(`${API_URL}/orgs/acme/operators/field-one-control/keys`, () =>
        HttpResponse.error(),
      ),
    );

    await renderPage({ pair: "field-one-control" });

    expect(screen.getByText("Cannot reach the API")).toBeInTheDocument();
    expect(
      screen.getByRole("list", { name: "Connect Field cluster one steps" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Issue key" })).toBeNull();
  });

  it("says the caller cannot issue a pair's keys where the server denies both actions", async () => {
    pairedWithControlService();
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:register" || action === "operator:reissue"
          ? "deny"
          : "allow",
      ),
    );

    await renderPage({ pair: "field-one-control" });

    expect(
      screen.getByText("No policy lets you issue these identities a key"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Issue/ })).toBeNull();
  });

  it("opens on issuing a key for an operator that holds none", async () => {
    keysAre();

    await renderPage();

    expect(currentStep()).toHaveTextContent("Issue a key");
  });

  it("resumes on placing a key that has never signed in", async () => {
    keysAre({ ...fieldOneKey, lastExchangedAt: undefined });

    await renderPage();

    expect(currentStep()).toHaveTextContent("Place the key");
  });

  it("opens connected for a key that has signed in", async () => {
    await renderPage();

    expect(
      screen.getByText("Field cluster one is connected"),
    ).toBeInTheDocument();
  });

  it("opens on issuing a replacement when asked to rotate", async () => {
    await renderPage({ rotate: "" });

    expect(currentStep()).toHaveTextContent("Issue a key");
    expect(screen.getByRole("radio", { name: /Rotate/ })).toBeChecked();
  });

  it("offers a first key to a caller garam allows re-issuing alone", async () => {
    keysAre();
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:register" ? "deny" : "allow",
      ),
    );

    await renderPage();

    expect(
      screen.getByRole("button", { name: "Issue key" }),
    ).toBeInTheDocument();
  });

  it("says the caller cannot issue keys where the server denies both actions", async () => {
    keysAre();
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:register" || action === "operator:reissue"
          ? "deny"
          : "allow",
      ),
    );

    await renderPage();

    expect(
      screen.getByText("No policy lets you issue this identity a key"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Issue key" })).toBeNull();
  });

  it("says the keys were refused rather than offering a first key", async () => {
    server.use(
      http.get(KEYS, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderPage();

    expect(
      screen.getByText("You can't view this identity or its keys"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Issue key" })).toBeNull();
  });

  it("says the API is unreachable rather than starting over", async () => {
    server.use(http.get(KEYS, () => HttpResponse.error()));

    await renderPage();

    expect(screen.getByText("Cannot reach the API")).toBeInTheDocument();
    // The wizard stays mounted around the notice, so the render that
    // follows issuing never unmounts a key it is showing.
    expect(
      screen.getByRole("list", { name: "Connect Field cluster one steps" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Issue key" })).toBeNull();
  });
});
