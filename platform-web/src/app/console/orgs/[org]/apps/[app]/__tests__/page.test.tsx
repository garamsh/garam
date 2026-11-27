import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import InstallationPage from "@/app/console/orgs/[org]/apps/[app]/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import {
  API_URL,
  answerPermissionChecks,
  botConnector,
  fieldBot,
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
  usePathname: () => "/console/orgs/acme/apps/garam-bot-connector",
  useRouter: () => ({ refresh: () => {} }),
}));

function renderPage(tab?: string) {
  return renderServerComponent(
    InstallationPage({
      params: Promise.resolve({ org: "acme", app: "garam-bot-connector" }),
      searchParams: Promise.resolve(tab ? { tab } : {}),
    }),
    { wrapper: TooltipProvider },
  );
}

function fact(term: string) {
  return screen.getByText(term, { selector: "dt" })
    .nextElementSibling as HTMLElement;
}

describe("installation page", () => {
  it("titles the page with the app's name, offers its slug to copy, and prints no GRN or ISO date", async () => {
    const { container } = await renderPage();

    expect(
      screen.getByRole("heading", { level: 1, name: "Bot connector" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("group", { name: "Identifier" })).toHaveTextContent(
      "garam-bot-connector",
    );
    expect(container.textContent).not.toContain("grn:");
    expect(container.textContent).not.toMatch(/\d{4}-\d{2}-\d{2}T/);
  });

  it("says what was installed and over what", async () => {
    await renderPage();

    expect(fact("Scope")).toHaveTextContent("The whole organization");
    expect(fact("Actions it requests")).toHaveTextContent("None");
    expect(fact("Instances")).toHaveTextContent("Many");
    expect(fact("Installed")).toHaveTextContent("by Alice");
  });

  it("names the projects of a project-scoped installation", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/installations/garam-bot-connector`, () =>
        HttpResponse.json({
          grn: "grn:acme/iam/installation/garam-bot-connector",
          orgGrn: "grn:root/iam/org/acme",
          appGrn: botConnector.grn,
          incarnation: "1",
          botHosting: false,
          projects: ["grn:acme/iam/project/frontend"],
          createdBy: "grn:root/iam/user/alice",
          createdAt: "2026-11-02T08:00:00Z",
        }),
      ),
    );

    await renderPage();

    expect(fact("Scope")).toHaveTextContent("Frontend");
  });

  it("lists its instances with the kind as text and the last contact", async () => {
    await renderPage("instances");

    const row = screen.getByRole("row", { name: /Field bot/ });
    expect(
      within(row).getByRole("link", { name: "Field bot" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/apps/garam-bot-connector/i-1a2b3c",
    );
    expect(row).toHaveTextContent(fieldBot.kind);
    expect(row).not.toHaveTextContent("Never");
    expect(
      screen.getByRole("button", { name: "Create an instance" }),
    ).toBeInTheDocument();
  });

  it("says never for an instance that has made no contact, and not read where it could not be read", async () => {
    server.use(
      http.get(
        `${API_URL}/orgs/acme/installations/garam-bot-connector/instances/i-1a2b3c/report`,
        () => HttpResponse.json({ lastContactAt: null }),
      ),
    );
    await renderPage("instances");
    expect(screen.getByRole("row", { name: /Field bot/ })).toHaveTextContent(
      "Never",
    );
  });

  it("leaves creating an instance out where the app runs none", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/apps/garam-bot-connector`, () =>
        HttpResponse.json({ ...botConnector, instances: "none" }),
      ),
    );

    await renderPage("instances");

    expect(
      screen.queryByRole("button", { name: "Create an instance" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/runs no instance/)).toBeInTheDocument();
  });

  it("leaves creating an instance out where the app runs one and it exists", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/apps/garam-bot-connector`, () =>
        HttpResponse.json({ ...botConnector, instances: "one" }),
      ),
    );

    await renderPage("instances");

    expect(
      screen.queryByRole("button", { name: "Create an instance" }),
    ).not.toBeInTheDocument();
  });

  it("leaves creating and uninstalling out for a caller the server denies them", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "iam:instance.register" ||
        action === "iam:installation.delete"
          ? "deny"
          : "allow",
      ),
    );

    await renderPage("instances");

    expect(
      screen.queryByRole("button", { name: "Create an instance" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Uninstall app" }),
    ).not.toBeInTheDocument();
    // Control: the instances are still listed.
    expect(screen.getByRole("link", { name: "Field bot" })).toBeInTheDocument();
  });

  it("offers uninstalling in the danger zone", async () => {
    await renderPage("instances");

    expect(
      screen.getByRole("button", { name: "Uninstall app" }),
    ).toBeInTheDocument();
  });

  it("says garam did not show the instances, not that there are none, when refused", async () => {
    server.use(
      http.get(
        `${API_URL}/orgs/acme/installations/garam-bot-connector/instances`,
        () =>
          HttpResponse.json(
            { kind: "permission_denied", message: "no" },
            { status: 403 },
          ),
      ),
    );

    await renderPage("instances");

    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText(/this app's instances/)).toBeInTheDocument();
  });
});
