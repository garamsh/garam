import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import OrgSettingsPage from "@/app/console/orgs/[org]/settings/page";
import { renderServerComponent } from "@/testing/render";
import {
  API_URL,
  answerPermissionChecks,
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
  useRouter: () => ({ refresh: () => {} }),
}));

function params(org: string) {
  return { params: Promise.resolve({ org }) };
}

const refused = (status: number) =>
  HttpResponse.json({ kind: "permission_denied", message: "no" }, { status });

describe("organization settings", () => {
  it("offers the name to edit, the identifier to copy and the danger zone", async () => {
    const { container } = await renderServerComponent(
      OrgSettingsPage(params("acme")),
    );

    expect(
      screen.getByRole("heading", { level: 1, name: "Settings" }),
    ).toBeInTheDocument();
    const general = screen.getByRole("region", { name: "General" });
    expect(within(general).getByLabelText("Name")).toHaveValue("Acme");
    expect(
      within(general).getByRole("button", {
        name: "Copy the identifier of Acme: acme",
      }),
    ).toBeInTheDocument();
    const danger = screen.getByRole("region", { name: "Danger zone" });
    expect(
      within(danger).getByRole("button", { name: "Delete organization" }),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("asks the server about renaming and deleting in one check", async () => {
    const asked: string[][] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/permission-checks`,
        async ({ request }) => {
          const { checks } = (await request.json()) as {
            checks: { action: string }[];
          };
          asked.push(checks.map((check) => check.action));
          return HttpResponse.json({
            results: checks.map((check) => ({ ...check, decision: "allow" })),
          });
        },
      ),
    );

    await renderServerComponent(OrgSettingsPage(params("acme")));

    expect(asked).toEqual([["org:update", "org:delete"]]);
  });

  it.each(["allow", "unknown"] as const)(
    "offers both controls when the check answers %s",
    async (decision) => {
      server.use(answerPermissionChecks(() => decision));

      await renderServerComponent(OrgSettingsPage(params("acme")));

      expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Delete organization" }),
      ).toBeInTheDocument();
    },
  );

  it("leaves the rename out when only the rename is denied, and keeps the name readable", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "org:update" ? "deny" : "allow",
      ),
    );

    await renderServerComponent(OrgSettingsPage(params("acme")));

    const general = screen.getByRole("region", { name: "General" });
    expect(within(general).queryByRole("button", { name: "Save" })).toBeNull();
    expect(within(general).getByText("Acme")).toBeInTheDocument();
    expect(
      within(general).getByText(/does not let you rename this organization/),
    ).toBeInTheDocument();
    // The control: the delete, which the server allowed, is still
    // offered.
    expect(
      screen.getByRole("button", { name: "Delete organization" }),
    ).toBeInTheDocument();
  });

  it("leaves the danger zone out when only the delete is denied", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "org:delete" ? "deny" : "allow",
      ),
    );

    await renderServerComponent(OrgSettingsPage(params("acme")));

    expect(screen.queryByRole("region", { name: "Danger zone" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Delete organization" }),
    ).toBeNull();
    // The control: the rename, which the server allowed, is still
    // offered.
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("keeps the delete control when only the preview is refused", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/deletion-preview`, () => refused(403)),
    );

    await renderServerComponent(OrgSettingsPage(params("acme")));

    expect(
      screen.getByRole("button", { name: "Delete organization" }),
    ).toBeInTheDocument();
  });

  it.each([403, 404])(
    "renders not-found when the organization read answers %i",
    async (status) => {
      server.use(http.get(`${API_URL}/orgs/acme`, () => refused(status)));

      await expect(
        renderServerComponent(OrgSettingsPage(params("acme"))),
      ).rejects.toThrow("NEXT_NOT_FOUND");
    },
  );

  it("says the API is unreachable rather than offering a form", async () => {
    server.use(http.get(`${API_URL}/*`, () => HttpResponse.error()));

    await renderServerComponent(OrgSettingsPage(params("acme")));

    expect(screen.getByText("Cannot reach the API")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Delete organization" }),
    ).toBeNull();
  });

  it("sends a rejected session to the login page", async () => {
    server.use(...signedOut());

    await expect(
      renderServerComponent(OrgSettingsPage(params("acme"))),
    ).rejects.toThrow("NEXT_REDIRECT:/login");
  });
});
