import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PermissionsPage from "@/app/console/orgs/[org]/permissions/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import { API_URL, answerPermissionChecks } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const POLICIES = `${API_URL}/orgs/acme/policies`;

// The console layout mounts the provider a `<Time>` needs.
function renderPolicies() {
  return renderServerComponent(
    PermissionsPage({ params: Promise.resolve({ org: "acme" }) }),
    { wrapper: TooltipProvider },
  );
}

function refuse() {
  return HttpResponse.json({ kind: "denied", message: "no" }, { status: 403 });
}

describe("permissions page", () => {
  it("lists the policies by name with their state and default version", async () => {
    const { container } = await renderPolicies();

    const row = screen.getByRole("row", {
      // Presence: a row's accessible name is all of its cells.
      name: /Deny project deletion/,
    });
    expect(within(row).getByText("Active")).toBeInTheDocument();
    expect(within(row).getByText("v1")).toBeInTheDocument();
    // The count stands beside the title, read from the list.
    // Presence: the count is its own text node, so the accessible name
    // joins it to the title with no space a string could name.
    expect(
      screen.getByRole("heading", { level: 1, name: /^Permissions\s*2$/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Deny project deletion" }),
    ).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/d1e2f3a4b5c6d7e8",
    );
    expect(screen.getByRole("link", { name: "Attachments" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/attachments",
    );
    expect(container.textContent).not.toContain("grn:");
  });

  it("says the organization has no policies rather than showing an empty table", async () => {
    server.use(http.get(POLICIES, () => HttpResponse.json([])));

    await renderPolicies();

    expect(screen.getByText("No policies")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("says the policies are refused rather than empty when garam answers 403", async () => {
    // The control: answered, the same page lists the policies.
    const allowed = await renderPolicies();
    expect(
      screen.getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(http.get(POLICIES, refuse));

    await renderPolicies();

    expect(
      screen.getByText("You can't view this organization's policies"),
    ).toBeInTheDocument();
    expect(screen.queryByText("No policies")).not.toBeInTheDocument();
  });

  it("renders not-found when the organization is not the caller's", async () => {
    // The control: the same page, the organization read, renders.
    const allowed = await renderPolicies();
    // Presence: the count is its own text node, so the accessible name
    // joins it to the title with no space a string could name.
    expect(
      screen.getByRole("heading", { level: 1, name: /^Permissions\s*2$/ }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(`${API_URL}/orgs/acme`, refuse),
      http.get(POLICIES, refuse),
    );

    await expect(renderPolicies()).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says the API is unreachable rather than showing no policies", async () => {
    // The control: answered, the same page lists the policies.
    const allowed = await renderPolicies();
    expect(
      screen.getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(http.get(POLICIES, () => HttpResponse.error()));

    await renderPolicies();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("No policies")).not.toBeInTheDocument();
  });

  it("withdraws Create policy only where the server denies it", async () => {
    // The control: allowed, the action is offered.
    const allowed = await renderPolicies();
    expect(screen.getByRole("link", { name: "Create policy" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/new",
    );
    allowed.unmount();

    // An unanswered check renders the control; the server decides.
    server.use(answerPermissionChecks(() => "unknown"));
    const unknown = await renderPolicies();
    expect(
      screen.getByRole("link", { name: "Create policy" }),
    ).toBeInTheDocument();
    unknown.unmount();

    server.use(answerPermissionChecks(() => "deny"));
    await renderPolicies();
    expect(
      screen.queryByRole("link", { name: "Create policy" }),
    ).not.toBeInTheDocument();
    // The list itself still reads.
    expect(
      screen.getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
  });
});
