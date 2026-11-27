import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import AppsPage from "@/app/console/orgs/[org]/apps/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { API_URL, answerPermissionChecks } from "@/testing/mocks/handlers";
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

function renderPage() {
  return renderServerComponent(
    AppsPage({ params: Promise.resolve({ org: "acme" }) }),
    { wrapper: TooltipProvider },
  );
}

/** The table row that names an app. */
function rowOf(name: string) {
  return screen.getByRole("row", { name: new RegExp(name) });
}

describe("apps list", () => {
  it("lists each app by name with its slug, and prints no GRN", async () => {
    const { container } = await renderPage();

    expect(
      screen.getByRole("heading", { level: 1, name: /Apps/ }),
    ).toHaveTextContent("Apps2");
    expect(rowOf("Bot connector")).toHaveTextContent("garam-bot-connector");
    expect(rowOf("Deploy helper")).toHaveTextContent("deploy-helper");
    expect(container.textContent).not.toContain("grn:");
  });

  it("opens an installed app's installation and offers installing the others", async () => {
    await renderPage();

    const installed = within(rowOf("Bot connector"));
    expect(installed.getByText("Installed")).toBeInTheDocument();
    expect(
      installed.getByRole("link", { name: "Open Bot connector" }),
    ).toHaveAttribute("href", "/console/orgs/acme/apps/garam-bot-connector");
    expect(
      installed.queryByRole("button", { name: /Install/ }),
    ).not.toBeInTheDocument();
    expect(
      within(rowOf("Deploy helper")).getByRole("button", {
        name: "Install Deploy helper",
      }),
    ).toBeInTheDocument();
  });

  it("says how many instances each app runs", async () => {
    await renderPage();

    expect(rowOf("Bot connector")).toHaveTextContent("Many");
    expect(rowOf("Deploy helper")).toHaveTextContent("One");
  });

  it("leaves installing out for a caller the server denies it", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "iam:installation.create" ? "deny" : "allow",
      ),
    );

    await renderPage();

    expect(
      within(rowOf("Deploy helper")).queryByRole("button", {
        name: /Install/,
      }),
    ).not.toBeInTheDocument();
    expect(
      within(rowOf("Deploy helper")).getByText("Not installed"),
    ).toBeInTheDocument();
  });

  it("offers installing when the server cannot decide", async () => {
    server.use(answerPermissionChecks(() => "unknown"));

    await renderPage();

    expect(
      screen.getByRole("button", { name: "Install Deploy helper" }),
    ).toBeInTheDocument();
  });

  it("says the apps were not read, not that there are none, when garam refuses", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/apps`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderPage();

    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText(/this organization's apps/)).toBeInTheDocument();
  });

  it("says garam did not answer when it is unreachable", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/apps`, () => HttpResponse.error()),
    );

    await renderPage();

    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText(/this organization's apps/)).toBeInTheDocument();
  });
});
