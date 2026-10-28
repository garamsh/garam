import { render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { CreateOrgForm } from "@/app/console/orgs/new/_components/create-org-form";
import { enter } from "@/testing/enter";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";
import { REFUSAL_COPY } from "@/lib/refusal-copy";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));

// `revalidatePath` needs a request scope the test does not run in; the
// action's own answer is what the form renders.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

/** Submits the form and answers with queries scoped to its render. */
async function submitName(name: string) {
  const user = userEvent.setup();
  const { container } = render(<CreateOrgForm />);
  const form = within(container);
  await enter(user, form.getByLabelText("Name"), name);
  await user.click(form.getByRole("button", { name: "Create organization" }));
  return form;
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

    const form = await submitName("Acme");

    expect(
      await form.findByText(REFUSAL_COPY.alreadyExists),
    ).toBeInTheDocument();
    expect(form.queryByText("that name is taken")).toBeNull();
  });

  it("keeps the form when the API never answers", async () => {
    server.use(http.post(`${API_URL}/orgs`, () => HttpResponse.error()));

    const form = await submitName("Acme");

    expect(await form.findByRole("alert")).toBeInTheDocument();
    expect(
      form.getByRole("button", { name: "Create organization" }),
    ).toBeInTheDocument();
  });
});
