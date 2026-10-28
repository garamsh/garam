import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import PersonPage from "@/app/console/orgs/[org]/people/[person]/page";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { renderServerComponent } from "@/testing/render";
import { API_URL } from "@/testing/mocks/handlers";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
  redirect: (url: string) => {
    throw new Error(`NEXT_REDIRECT:${url}`);
  },
  usePathname: () => "/console/orgs/acme/people/alice",
}));

function renderPerson(person = "alice") {
  return renderServerComponent(
    PersonPage({ params: Promise.resolve({ org: "acme", person }) }),
    { wrapper: TooltipProvider },
  );
}

describe("a person's page", () => {
  it("names the person, offers the identifier to copy, and prints no GRN", async () => {
    const { container } = await renderPerson();

    expect(
      screen.getByRole("heading", { level: 1, name: "Alice" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: "Person identifier" }),
    ).toHaveTextContent("alice");
    expect(container.textContent).not.toContain("grn:");
  });

  it("carries Your access, asked of garam", async () => {
    await renderPerson();

    const panel = screen.getByRole("region", { name: "Your access" });
    expect(panel).toHaveTextContent("Invite people");
    expect(panel).toHaveTextContent("Your organization role owner");
  });

  it("answers not found for someone the organization does not hold", async () => {
    await expect(renderPerson("nobody")).rejects.toThrow("NEXT_NOT_FOUND");
  });

  it("says a refused list of people rather than not found", async () => {
    server.use(
      http.get(`${API_URL}/orgs/acme/members`, () =>
        HttpResponse.json(
          { kind: "permission_denied", message: "no" },
          { status: 403 },
        ),
      ),
    );

    await renderPerson();

    expect(
      screen.getByText("You can't view this organization's people"),
    ).toBeInTheDocument();
  });
});
