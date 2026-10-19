import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";

import { ConsoleBreadcrumb } from "@/app/console/_components/console-breadcrumb";
import { API_URL } from "@/testing/mocks/handlers";
import { renderServerComponent } from "@/testing/render";
import { server } from "@/testing/setup";

vi.mock("next/navigation", () => ({
  usePathname: () => "/console/orgs/acme/people",
}));

describe("console breadcrumb", () => {
  it("labels the organization with the name the API gave", async () => {
    await renderServerComponent(ConsoleBreadcrumb());

    expect(screen.getByRole("link", { name: "Acme" })).toBeInTheDocument();
  });

  it("falls back to the slug when the API is unreachable", async () => {
    server.use(http.get(`${API_URL}/orgs`, () => HttpResponse.error()));

    await renderServerComponent(ConsoleBreadcrumb());

    expect(screen.getByRole("link", { name: "acme" })).toBeInTheDocument();
    expect(screen.getByText("People")).toBeInTheDocument();
  });
});
