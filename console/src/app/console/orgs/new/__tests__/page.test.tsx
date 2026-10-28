import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import NewOrgPage from "@/app/console/orgs/new/page";
import { renderServerComponent } from "@/testing/render";

describe("new organization page", () => {
  it("renders the create form", async () => {
    await renderServerComponent(Promise.resolve(<NewOrgPage />));

    expect(
      screen.getByRole("heading", { name: "New organization" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Create organization" }),
    ).toBeInTheDocument();
  });
});
