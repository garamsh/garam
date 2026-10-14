import { render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { RenameOrgForm } from "@/app/console/orgs/[org]/settings/_components/rename-org-form";
import { enter } from "@/testing/enter";
import { API_URL, acme } from "@/testing/mocks/handlers";
import { REFUSAL_COPY } from "@/lib/refusal-copy";
import { server } from "@/testing/setup";

// `revalidatePath` needs a request scope the test does not run in.
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

async function rename(to: string) {
  const user = userEvent.setup();
  const { container } = render(<RenameOrgForm org="acme" name="Acme" />);
  const form = within(container);
  const field = form.getByLabelText("Name");
  await user.clear(field);
  await enter(user, field, to);
  await user.click(form.getByRole("button", { name: "Save" }));
  return { form, field };
}

describe("rename organization form", () => {
  it("sends only the name and says what it was saved as, beside Save", async () => {
    const sent: unknown[] = [];
    server.use(
      http.patch(`${API_URL}/orgs/acme`, async ({ request }) => {
        const body = (await request.json()) as { name: string };
        sent.push(body);
        return HttpResponse.json({ ...acme, name: body.name });
      }),
    );

    const { form } = await rename("  Acme Corp ");

    expect(await form.findByRole("status")).toHaveTextContent(
      "Saved as Acme Corp",
    );
    expect(sent).toEqual([{ name: "Acme Corp" }]);
  });

  it("puts a refusal on the name field and keeps what was typed", async () => {
    server.use(
      http.patch(`${API_URL}/orgs/acme`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "org: denied" },
          { status: 403 },
        ),
      ),
    );

    const { form, field } = await rename("Acme Corp");

    expect(await form.findByRole("alert")).toHaveTextContent(
      REFUSAL_COPY.permissionDenied,
    );
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveAccessibleDescription(REFUSAL_COPY.permissionDenied);
    expect(field).toHaveValue("Acme Corp");
    expect(form.queryByText("org: denied")).toBeNull();
  });

  it("offers no Save for an empty name", async () => {
    const user = userEvent.setup();
    const { container } = render(<RenameOrgForm org="acme" name="Acme" />);
    const form = within(container);

    await user.clear(form.getByLabelText("Name"));

    expect(form.getByRole("button", { name: "Save" })).toBeDisabled();
  });
});
