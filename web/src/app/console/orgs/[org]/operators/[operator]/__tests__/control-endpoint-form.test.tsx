import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ControlEndpointForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/control-endpoint-form";
import { TooltipProvider } from "@/components/ui/tooltip";
import { enter } from "@/testing/enter";
import { API_URL, fieldOne } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const ENDPOINT = `${API_URL}/orgs/acme/operators/field-one/control-endpoint`;

function renderForm(endpoint?: string) {
  return render(
    <TooltipProvider>
      <ControlEndpointForm
        org="acme"
        operator="field-one"
        endpoint={endpoint}
      />
    </TooltipProvider>,
  );
}

describe("control endpoint form", () => {
  it("says no control service is reached while none is registered", () => {
    renderForm();

    expect(screen.getByText(/No endpoint is registered/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Clear endpoint" })).toBeNull();
  });

  it("saves an endpoint and says so beside the button", async () => {
    const put: unknown[] = [];
    server.use(
      http.put(ENDPOINT, async ({ request }) => {
        put.push(await request.json());
        return HttpResponse.json({
          ...fieldOne,
          controlEndpoint: "https://control.example.com",
        });
      }),
    );
    const user = userEvent.setup();
    renderForm();

    await enter(
      user,
      screen.getByRole("textbox", { name: "Endpoint" }),
      "https://control.example.com",
    );
    await user.click(screen.getByRole("button", { name: "Save endpoint" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Endpoint saved",
    );
    expect(put).toHaveLength(1);
  });

  it("attaches garam's refusal to the endpoint field", async () => {
    server.use(
      http.put(ENDPOINT, () =>
        HttpResponse.json(
          { kind: "invalid_argument", message: "control endpoint: not https" },
          { status: 400 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderForm();
    const field = screen.getByRole("textbox", { name: "Endpoint" });

    await enter(user, field, "https://control.example.com");
    await user.click(screen.getByRole("button", { name: "Save endpoint" }));

    await screen.findByRole("alert");
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(field).toHaveValue("https://control.example.com");
  });
});
