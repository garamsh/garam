import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreateProjectForm } from "@/app/console/orgs/[org]/projects/_components/create-project-form";
import { enter } from "@/testing/enter";
import { API_URL, frontend } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** What the form creates, and what garam answers for it. */
const RESOURCES = [
  {
    kind: "project",
    path: `${API_URL}/orgs/acme/projects`,
    heading: "Create a project",
    label: "Project name",
    submit: "Create project",
    typed: "Frontend",
    answer: frontend,
    identifier: "frontend",
  },
] as const;

describe.each(RESOURCES)(
  "create $kind form",
  ({ kind, path, heading, label, submit, typed, answer, identifier }) => {
    /** Records every body the form posts, and answers as garam does. */
    function recordCreates() {
      const posted: unknown[] = [];
      server.use(
        http.post(path, async ({ request }) => {
          posted.push(await request.json());
          return HttpResponse.json(answer, { status: 201 });
        }),
      );
      return posted;
    }

    it("asks for a name and nothing else", () => {
      render(<CreateProjectForm org="acme" />);

      expect(
        screen.getByRole("heading", { name: heading }),
      ).toBeInTheDocument();
      expect(screen.getByLabelText(label)).toBeInTheDocument();
      expect(screen.getAllByRole("textbox")).toHaveLength(1);
    });

    it("posts the name typed into it, confirms it by name, and offers the identifier to copy", async () => {
      const posted = recordCreates();
      const user = userEvent.setup();
      const { container } = render(<CreateProjectForm org="acme" />);
      const form = within(container);

      await enter(user, form.getByLabelText(label), typed);
      await user.click(form.getByRole("button", { name: submit }));

      // The sentence names the project; the identifier is no part of
      // it, and reaches a person only through the copy control.
      expect(await form.findByText(`Created ${typed}.`)).toBeInTheDocument();
      expect(
        form.getByRole("button", {
          name: `Copy the identifier of ${typed}: ${identifier}`,
        }),
      ).toBeInTheDocument();
      expect(posted).toEqual([{ name: typed }]);
      // The console addresses a resource by the identifier segment and
      // prints no GRN (ADR-0011), the confirmation included.
      expect(container.textContent).not.toContain("grn:");
    });

    it("renders the refusal the API made, in place of a confirmation", async () => {
      server.use(
        http.post(path, () =>
          HttpResponse.json(
            {
              kind: "invalid_argument",
              message: `this organization already holds a ${kind} named ${typed}`,
            },
            { status: 400 },
          ),
        ),
      );
      const user = userEvent.setup();
      const form = within(render(<CreateProjectForm org="acme" />).container);

      await enter(user, form.getByLabelText(label), typed);
      await user.click(form.getByRole("button", { name: submit }));

      // Presence: the server's text stands inside the generic sentence
      // for a refusal nothing maps.
      expect(await form.findByRole("alert")).toHaveTextContent(
        `this organization already holds a ${kind} named ${typed}`,
      );
      expect(form.queryByText(identifier)).not.toBeInTheDocument();
    });
  },
);
