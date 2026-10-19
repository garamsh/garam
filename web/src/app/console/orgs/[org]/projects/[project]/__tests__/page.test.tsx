import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import ProjectPage from "@/app/console/orgs/[org]/projects/[project]/page";
import { TooltipProvider } from "@/components/ui/tooltip";
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
  usePathname: () => "/console/orgs/acme/projects/frontend",
  useRouter: () => ({ refresh: () => {} }),
}));

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

function renderProject() {
  return renderServerComponent(
    ProjectPage({
      params: Promise.resolve({ org: "acme", project: "frontend" }),
    }),
    { wrapper: TooltipProvider },
  );
}

describe("a project's page", () => {
  it("names the project and lists its members by name", async () => {
    const { container } = await renderProject();

    expect(
      screen.getByRole("heading", { level: 1, name: "Frontend" }),
    ).toBeInTheDocument();
    const members = screen.getByRole("table", { name: "Members of Frontend" });
    expect(within(members).getByText("Alice")).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("adds a member to the project, not to a team", async () => {
    const added: string[] = [];
    server.use(
      http.post(
        `${API_URL}/orgs/acme/projects/frontend/members`,
        async ({ request }) => {
          const { memberGrn } = (await request.json()) as {
            memberGrn: string;
          };
          added.push(memberGrn);
          return HttpResponse.json({ memberGrn }, { status: 201 });
        },
      ),
    );
    const user = userEvent.setup();
    await renderProject();

    await user.click(screen.getByRole("combobox", { name: "Person or agent" }));
    // Presence: an option's name goes on to carry the identifier.
    await user.click(
      await screen.findByRole("option", { name: /^Unnamed agent / }),
    );
    await user.click(screen.getByRole("button", { name: "Add to project" }));

    expect(await screen.findByText("Added Unnamed agent.")).toBeInTheDocument();
    expect(added).toEqual(["grn:acme:default:agent:1c3d5e7f90a2b4c6"]);
  });

  it("deletes the project only once its name is typed", async () => {
    let deleted = 0;
    server.use(
      http.delete(`${API_URL}/orgs/acme/projects/frontend`, () => {
        deleted += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    await renderProject();

    await user.click(screen.getByRole("tab", { name: "Settings" }));
    await user.click(screen.getByRole("button", { name: "Delete project" }));
    const dialog = screen.getByRole("alertdialog", {
      name: "Delete Frontend?",
    });
    const confirm = within(dialog).getByRole("button", {
      name: "Delete project",
    });
    expect(confirm).toBeDisabled();
    expect(deleted).toBe(0);

    await user.type(within(dialog).getByRole("textbox"), "Frontend");
    expect(confirm).toBeEnabled();
  });

  it("leaves out the writes the server denies, and keeps them in Your access", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "project:delete" || action === "project:member-add"
          ? "deny"
          : "allow",
      ),
    );

    await renderProject();

    expect(
      screen.queryByRole("combobox", { name: "Person or agent" }),
    ).not.toBeInTheDocument();
    const access = screen.getByRole("region", { name: "Your access" });
    expect(access).toHaveTextContent(/Delete it.*Not allowed/);
    // Control: a write the server allows is still offered.
    expect(
      screen.getAllByRole("button", { name: /^Remove Alice/ }).length,
    ).toBeGreaterThan(0);
  });
});
