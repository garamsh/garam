import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RenameAgentForm } from "@/app/console/orgs/[org]/agents/[agent]/_components/rename-agent-form";
import { API_URL, fieldAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ONE = `${API_URL}/orgs/acme/agent-definitions/9f2ac1b40d8e7a35`;

describe("rename agent form", () => {
  it("keeps the attempted name through a refusal and saves it on retry", async () => {
    let refusing = true;
    server.use(
      http.patch(ONE, async ({ request }) => {
        if (refusing) {
          return HttpResponse.json(
            { kind: "permission_denied", message: "permission denied" },
            { status: 403 },
          );
        }
        const body = (await request.json()) as { name: string };
        return HttpResponse.json({ ...fieldAgent, name: body.name });
      }),
    );
    const user = userEvent.setup();
    render(
      <RenameAgentForm org="acme" agent="9f2ac1b40d8e7a35" name="Scout" />,
    );
    const name = screen.getByLabelText("Name");

    await user.clear(name);
    await user.type(name, "Ranger");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission denied",
    );
    expect(name).toHaveValue("Ranger");

    // The control: accepted, the same name is saved and said to be.
    refusing = false;
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Renamed to Ranger.",
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
