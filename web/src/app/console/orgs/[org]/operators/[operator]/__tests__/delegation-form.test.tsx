import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { DelegationForm } from "@/app/console/orgs/[org]/operators/[operator]/_components/delegation-form";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { Delegation } from "@/features/orgs";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const MANAGER = "grn:acme:default:operator:field-manager";
const delegation: Delegation = {
  grantId: "grant-1",
  operatorGrn: "grn:acme:default:operator:field-one",
  controllers: [MANAGER, "grn:acme:default:operator:gone"],
  operations: ["agent:create", "operator:read"],
  expiresAt: "2026-10-21T00:00:00Z",
  grantedBy: "grn:root:default:user:alice",
  grantedAt: "2026-10-14T00:00:00Z",
};
const controllers = [
  { grn: MANAGER, name: "Field manager", identifier: "field-manager" },
];

function renderForm(current: Delegation | null = delegation) {
  return render(
    <TooltipProvider>
      <DelegationForm
        org="acme"
        operator="field-one"
        delegation={current}
        controllers={controllers}
      />
    </TooltipProvider>,
  );
}

describe("delegation form", () => {
  it("names the grant's controllers and operations, never their GRNs", () => {
    const { container } = renderForm();

    expect(
      screen.getByText("Field manager", { selector: "li" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("An operator no longer registered"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Read operator status", { selector: "li" }),
    ).toBeInTheDocument();
    expect(container.textContent).not.toContain("grn:");
  });

  it("withdraws only once confirmed, naming the grant shown", async () => {
    const withdrawn: string[] = [];
    server.use(
      http.delete(
        `${API_URL}/orgs/acme/operators/field-one/delegation`,
        ({ request }) => {
          withdrawn.push(
            request.headers.get("if-match") ?? new URL(request.url).search,
          );
          return new HttpResponse(null, { status: 204 });
        },
      ),
    );
    const user = userEvent.setup();
    renderForm();

    await user.click(
      screen.getByRole("button", { name: "Withdraw delegation" }),
    );
    const dialog = screen.getByRole("alertdialog", {
      name: "Withdraw this delegation?",
    });
    expect(withdrawn).toEqual([]);
    await user.click(
      within(dialog).getByRole("button", { name: "Withdraw delegation" }),
    );

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Delegation withdrawn",
    );
    expect(withdrawn).toHaveLength(1);
  });

  it("says no delegation is held, and offers granting one", () => {
    renderForm(null);

    expect(screen.getByText(/holds no delegation/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Grant delegation" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Withdraw delegation" }),
    ).toBeNull();
  });
});
