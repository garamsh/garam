import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreateTeamForm } from "@/app/console/orgs/[org]/teams/new/_components/create-team-form";
import NewTeamPage from "@/app/console/orgs/[org]/teams/new/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, answerPermissionChecks } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@garam/platform-client/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

function params(org: string) {
  return { params: Promise.resolve({ org }) };
}

describe("new team page", () => {
  it("offers the name form where the server allows or cannot say", async () => {
    server.use(answerPermissionChecks(() => "unknown"));

    await renderServerComponent(NewTeamPage(params("acme")));

    expect(screen.getByLabelText("Team name")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Create team" }),
    ).toBeInTheDocument();
  });

  it("says a caller the server denies cannot create a team instead of a form", async () => {
    server.use(
      answerPermissionChecks((action) =>
        action === "group:create" ? "deny" : "allow",
      ),
    );

    await renderServerComponent(NewTeamPage(params("acme")));

    expect(screen.getByRole("status")).toHaveTextContent(
      "No policy lets you create teams",
    );
    expect(screen.queryByLabelText("Team name")).not.toBeInTheDocument();
  });
});

describe("create team form", () => {
  it("attaches a refusal to the name and keeps the name typed", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/groups`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "permission denied" },
          { status: 403 },
        ),
      ),
    );
    const user = userEvent.setup();
    render(<CreateTeamForm org="acme" />);

    const name = screen.getByLabelText("Team name");
    await user.type(name, "Platform");
    await user.click(screen.getByRole("button", { name: "Create team" }));

    await waitFor(() => expect(name).toHaveAttribute("aria-invalid", "true"));
    expect(name).toHaveAccessibleDescription(REFUSAL_COPY.permissionDenied);
    expect(name).toHaveValue("Platform");
  });
});
