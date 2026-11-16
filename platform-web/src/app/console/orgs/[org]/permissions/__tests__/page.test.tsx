import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PermissionsPage from "@/app/console/orgs/[org]/permissions/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  DENY_DELETES_ID,
  ENGINEERING_ADMINS_ID,
  answerPermissionChecks,
} from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

const POLICIES = `${API_URL}/orgs/acme/iam-policies`;

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
  it("lists each policy by name with its kind, state and default version", async () => {
    const { container } = await renderPolicies();

    const guardrail = screen.getByRole("row", {
      // Presence: a row's accessible name is all of its cells.
      name: /Deny project deletion/,
    });
    expect(within(guardrail).getByText("Guardrail")).toBeInTheDocument();
    expect(within(guardrail).getByText("Active")).toBeInTheDocument();
    expect(within(guardrail).getByText("v1")).toBeInTheDocument();
    const roleLink = screen.getByRole("row", { name: /Engineering admins/ });
    expect(within(roleLink).getByText("Role link")).toBeInTheDocument();
    expect(within(roleLink).getByText("Draft")).toBeInTheDocument();
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
      `/console/orgs/acme/permissions/${DENY_DELETES_ID}`,
    );
    expect(
      screen.getByRole("link", { name: "Engineering admins" }),
    ).toHaveAttribute(
      "href",
      `/console/orgs/acme/permissions/${ENGINEERING_ADMINS_ID}`,
    );
    expect(container.textContent).not.toContain("grn:");
  });

  it("offers Create policy unless the server denies every kind", async () => {
    // The control: one kind allowed and the rest denied still offers it.
    server.use(
      answerPermissionChecks(
        (action) => (action === "iam:policy.create" ? "allow" : "deny"),
        ["iam:policy.attach"],
      ),
    );
    const some = await renderPolicies();
    expect(screen.getByRole("link", { name: "Create policy" })).toHaveAttribute(
      "href",
      "/console/orgs/acme/permissions/new",
    );
    some.unmount();

    server.use(answerPermissionChecks(() => "deny", ["iam:policy.attach"]));
    await renderPolicies();
    expect(
      screen.queryByRole("link", { name: "Create policy" }),
    ).not.toBeInTheDocument();
    // The list itself is still read.
    expect(
      screen.getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
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

  it("says the same when the policy service times out, not an error page", async () => {
    // The control: answered, the same page lists the policies.
    const allowed = await renderPolicies();
    expect(
      screen.getByRole("link", { name: "Deny project deletion" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(
      http.get(POLICIES, () =>
        HttpResponse.json(
          { kind: "deadline_exceeded", message: "no answer in time" },
          { status: 504 },
        ),
      ),
    );

    await renderPolicies();

    expect(
      screen.getByRole("heading", { name: "Cannot reach the API" }),
    ).toBeInTheDocument();
  });
});
