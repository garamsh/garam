import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import NewPolicyPage from "@/app/console/orgs/[org]/permissions/new/page";
import { renderServerComponent } from "@/testing/render";
import { API_URL, answerPermissionChecks } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
}));
vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

function renderNew() {
  return renderServerComponent(
    NewPolicyPage({ params: Promise.resolve({ org: "acme" }) }),
  );
}

describe("new policy page", () => {
  it("offers the form unless the server denies creating a policy", async () => {
    // The control: allowed, the form is there.
    const allowed = await renderNew();
    expect(
      screen.getByRole("button", { name: "Create draft" }),
    ).toBeInTheDocument();
    allowed.unmount();

    server.use(answerPermissionChecks(() => "deny"));
    await renderNew();

    expect(
      screen.queryByRole("button", { name: "Create draft" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("You can't create policies here"),
    ).toBeInTheDocument();
  });

  it("offers the form when the check could not be asked", async () => {
    server.use(
      http.post(`${API_URL}/orgs/acme/permission-checks`, () =>
        HttpResponse.error(),
      ),
    );

    await renderNew();

    expect(
      screen.getByRole("button", { name: "Create draft" }),
    ).toBeInTheDocument();
  });
});
