import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PermissionsPage from "@/app/console/orgs/[org]/permissions/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL } from "@/testing/mocks/handlers";
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

function renderPolicies() {
  return renderServerComponent(
    PermissionsPage({ params: Promise.resolve({ org: "acme" }) }),
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
    expect(within(row).getByText("Version 1")).toBeInTheDocument();
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

    expect(
      screen.getByText("This organization has no policies."),
    ).toBeInTheDocument();
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
      screen.getByRole("heading", {
        name: "You cannot view this organization's policies",
      }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("This organization has no policies."),
    ).not.toBeInTheDocument();
  });

  it("renders not-found when the organization is not the caller's", async () => {
    // The control: the same page, the organization read, renders.
    const allowed = await renderPolicies();
    expect(
      screen.getByRole("heading", { level: 1, name: "Permissions" }),
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
    expect(
      screen.queryByText("This organization has no policies."),
    ).not.toBeInTheDocument();
  });
});
