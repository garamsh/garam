import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ConnectOperatorPage from "@/app/console/orgs/[org]/operators/[operator]/connect/page";
import { TooltipProvider } from "@/components/ui/tooltip";
import {
  API_URL,
  answerPermissionChecks,
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

function renderPage(rotate?: boolean) {
  return renderServerComponent(
    ConnectOperatorPage({
      params: Promise.resolve({ org: "acme", operator: "field-one" }),
      searchParams: Promise.resolve(rotate ? { rotate: "" } : {}),
    }),
    { wrapper: TooltipProvider },
  );
}

function currentStep() {
  return within(screen.getByRole("list", { name: /steps$/ }))
    .getAllByRole("listitem")
    .find((item) => item.getAttribute("aria-current") === "step");
}

describe("connect operator page", () => {
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
    await renderPage(true);

    expect(currentStep()).toHaveTextContent("Issue a key");
    expect(screen.getByRole("radio", { name: /Rotate/ })).toBeChecked();
  });

  it("says the caller cannot issue keys where the server denies it", async () => {
    keysAre();
    server.use(
      answerPermissionChecks((action) =>
        action === "operator:register" ? "deny" : "allow",
      ),
    );

    await renderPage();

    expect(
      screen.getByText("You cannot issue keys for this operator"),
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
      screen.getByText("You cannot set up this operator"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Issue key" })).toBeNull();
  });

  it("says the API is unreachable rather than starting over", async () => {
    server.use(http.get(KEYS, () => HttpResponse.error()));

    await renderPage();

    expect(screen.getByText("Cannot reach the API")).toBeInTheDocument();
  });
});
