import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreateProjectDialog } from "@/app/console/orgs/[org]/projects/_components/create-project-dialog";
import { enter } from "@/testing/enter";
import { API_URL, frontend } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the dialog renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const PATH = `${API_URL}/orgs/acme/projects`;

/** Records every body the dialog posts, and answers as garam does. */
function recordCreates() {
  const posted: unknown[] = [];
  server.use(
    http.post(PATH, async ({ request }) => {
      posted.push(await request.json());
      return HttpResponse.json(frontend, { status: 201 });
    }),
  );
  return posted;
}

async function openDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Create project" }));
  return within(screen.getByRole("dialog", { name: "Create a project" }));
}

describe("create project dialog", () => {
  it("asks for a name and nothing else", async () => {
    const user = userEvent.setup();
    render(<CreateProjectDialog org="acme" />);

    const dialog = await openDialog(user);

    expect(dialog.getByLabelText("Project name")).toBeInTheDocument();
    expect(dialog.getAllByRole("textbox")).toHaveLength(1);
  });

  it("posts the name, closes, and says what it created beside the button", async () => {
    const posted = recordCreates();
    const user = userEvent.setup();
    const { container } = render(<CreateProjectDialog org="acme" />);

    const dialog = await openDialog(user);
    await enter(user, dialog.getByLabelText("Project name"), "Frontend");
    await user.click(dialog.getByRole("button", { name: "Create project" }));

    // The sentence names the project; the identifier reaches a person
    // only through the copy control.
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Created Frontend.",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: "Copy the identifier of Frontend: frontend",
      }),
    ).toBeInTheDocument();
    expect(posted).toEqual([{ name: "Frontend" }]);
    expect(container.textContent).not.toContain("grn:");
  });

  it("attaches a refusal to the field and keeps the name typed", async () => {
    server.use(
      http.post(PATH, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<CreateProjectDialog org="acme" />);

    const dialog = await openDialog(user);
    await enter(user, dialog.getByLabelText("Project name"), "Frontend");
    await user.click(dialog.getByRole("button", { name: "Create project" }));

    const field = dialog.getByLabelText("Project name");
    expect(
      await dialog.findByText(
        "You don't have permission to do this. An owner or admin of the organization can grant it.",
      ),
    ).toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription(
      "You don't have permission to do this. An owner or admin of the organization can grant it.",
    );
    expect(field).toHaveValue("Frontend");
    // The refusal stays in the dialog: nothing beside the button says
    // a project was created.
    expect(screen.queryByText(/^Created/)).not.toBeInTheDocument();
  });
});
