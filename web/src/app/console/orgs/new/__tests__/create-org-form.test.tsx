import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreateOrgForm } from "@/app/console/orgs/new/_components/create-org-form";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

async function submitName(name: string) {
  const user = userEvent.setup();
  render(<CreateOrgForm />);
  await user.type(screen.getByLabelText("Name"), name);
  await user.click(screen.getByRole("button", { name: "Create organization" }));
}

describe("create organization form", () => {
  it("renders the backend's refusal rather than replacing the form", async () => {
    server.use(
      http.post(`${API_URL}/orgs`, () =>
        HttpResponse.json(
          { kind: "already_exists", message: "that name is taken" },
          { status: 409 },
        ),
      ),
    );

    await submitName("Acme");

    expect(await screen.findByText("that name is taken")).toBeInTheDocument();
  });

  it("keeps the form when the API never answers", async () => {
    server.use(http.post(`${API_URL}/orgs`, () => HttpResponse.error()));

    await submitName("Acme");

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Create organization" }),
    ).toBeInTheDocument();
  });
});
