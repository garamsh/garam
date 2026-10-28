import { render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RenameAgentForm } from "@/app/console/orgs/[org]/agents/[agent]/_components/rename-agent-form";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";
import { enter } from "@/testing/enter";
import { API_URL, reportedAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ONE = `${API_URL}/orgs/acme/agents/1c3d5e7f90a2b4c6`;

describe("rename agent form", () => {
  it("keeps the attempted name through a refusal and saves it trimmed on retry", async () => {
    let refusing = true;
    const posted: unknown[] = [];
    server.use(
      http.patch(ONE, async ({ request }) => {
        posted.push(await request.json());
        if (refusing) {
          return HttpResponse.json(
            { kind: "permission_denied", message: "permission denied" },
            { status: 403 },
          );
        }
        return HttpResponse.json({ ...reportedAgent, name: "Watson" });
      }),
    );
    const user = userEvent.setup();
    const { container } = render(
      <RenameAgentForm org="acme" agent="1c3d5e7f90a2b4c6" name="Sherlock" />,
    );
    const form = within(container);
    const name = form.getByLabelText("Name");

    await user.clear(name);
    await enter(user, name, "  Watson ");
    await user.click(form.getByRole("button", { name: "Save name" }));

    // The refusal is the name field's error, read with it.
    await vi.waitFor(() =>
      expect(name).toHaveAccessibleDescription(
        expect.stringContaining(REFUSAL_COPY.permissionDenied),
      ),
    );
    expect(name).toHaveAttribute("aria-invalid", "true");
    expect(name).toHaveValue("  Watson ");

    // The control: accepted, the same name is saved and said to be.
    refusing = false;
    await user.click(form.getByRole("button", { name: "Save name" }));

    expect(await form.findByRole("status")).toHaveTextContent(
      "Renamed to Watson.",
    );
    expect(name).not.toHaveAttribute("aria-invalid");
    // Both attempts carried the name trimmed, as garam stores it.
    expect(posted).toEqual([{ name: "Watson" }, { name: "Watson" }]);
  });

  it("clears the name when the field is left empty", async () => {
    const posted: unknown[] = [];
    server.use(
      http.patch(ONE, async ({ request }) => {
        posted.push(await request.json());
        return HttpResponse.json(reportedAgent);
      }),
    );
    const user = userEvent.setup();
    const { container } = render(
      <RenameAgentForm org="acme" agent="1c3d5e7f90a2b4c6" name="Sherlock" />,
    );
    const form = within(container);

    await user.clear(form.getByLabelText("Name"));
    await user.click(form.getByRole("button", { name: "Save name" }));

    expect(await form.findByRole("status")).toHaveTextContent("Name cleared.");
    expect(posted).toEqual([{ name: null }]);
  });
});
