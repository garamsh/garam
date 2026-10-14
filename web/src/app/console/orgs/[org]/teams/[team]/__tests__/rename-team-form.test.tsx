import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RenameTeamForm } from "@/app/console/orgs/[org]/teams/[team]/_components/rename-team-form";
import { API_URL, engineering } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const GROUP = `${API_URL}/orgs/acme/groups/eng`;

describe("rename team form", () => {
  it("says the saved name beside the Save", async () => {
    server.use(
      http.patch(GROUP, async ({ request }) => {
        const { name } = (await request.json()) as { name: string };
        return HttpResponse.json({ ...engineering, name });
      }),
    );
    const user = userEvent.setup();
    render(<RenameTeamForm org="acme" team="eng" name="Engineering" />);

    const name = screen.getByLabelText("Team name");
    await user.clear(name);
    await user.type(name, "Platform");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Renamed to Platform.",
    );
    expect(name).not.toHaveAttribute("aria-invalid");
  });

  it("attaches a refusal to the name and keeps the name typed", async () => {
    server.use(
      http.patch(GROUP, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<RenameTeamForm org="acme" team="eng" name="Engineering" />);

    const name = screen.getByLabelText("Team name");
    await user.clear(name);
    await user.type(name, "Platform");
    await user.click(screen.getByRole("button", { name: "Save name" }));

    await waitFor(() => expect(name).toHaveAttribute("aria-invalid", "true"));
    expect(name).toHaveAccessibleDescription(REFUSAL_COPY.permissionDenied);
    expect(name).toHaveValue("Platform");
    expect(screen.getByRole("status")).toHaveTextContent("");
  });
});
