import { render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RenameAgentForm } from "@/app/console/orgs/[org]/agents/[agent]/_components/rename-agent-form";
import { enter } from "@/testing/enter";
import { API_URL, fieldAgent } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ONE = `${API_URL}/orgs/acme/agent-definitions/9f2ac1b40d8e7a35`;

describe("rename agent form", () => {
  it("keeps the attempted name through a refusal and saves it on retry", async () => {
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
        return HttpResponse.json({ ...fieldAgent, name: "Ranger" });
      }),
    );
    const user = userEvent.setup();
    const { container } = render(
      <RenameAgentForm org="acme" agent="9f2ac1b40d8e7a35" name="Scout" />,
    );
    const form = within(container);
    const name = form.getByLabelText("Name");

    await user.clear(name);
    await enter(user, name, "Ranger");
    await user.click(form.getByRole("button", { name: "Save name" }));

    // The refusal is the name field's error, read with it.
    await vi.waitFor(() =>
      expect(name).toHaveAccessibleDescription(REFUSAL_COPY.permissionDenied),
    );
    expect(name).toHaveAttribute("aria-invalid", "true");
    expect(name).toHaveValue("Ranger");

    // The control: accepted, the same name is saved and said to be.
    refusing = false;
    await user.click(form.getByRole("button", { name: "Save name" }));

    expect(await form.findByRole("status")).toHaveTextContent(
      "Renamed to Ranger.",
    );
    expect(name).not.toHaveAttribute("aria-invalid");
    expect(form.queryByText(REFUSAL_COPY.permissionDenied)).toBeNull();
    // Both attempts carried the name as typed.
    expect(posted).toEqual([{ name: "Ranger" }, { name: "Ranger" }]);
  });
});
