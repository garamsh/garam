import { render, screen } from "@testing-library/react";
import { renderToString } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import { AgentSettingsForm } from "@/app/console/orgs/[org]/agents/[agent]/_components/agent-settings-form";

vi.mock("next/cache", () => ({ revalidatePath: () => {} }));

const form = (
  <AgentSettingsForm
    org="acme"
    agent="9f2ac1b40d8e7a35"
    revision={4}
    values={{ model: "haiku" }}
  />
);

describe("agent settings form", () => {
  it("submits nothing before hydration: the server render disables Save settings", () => {
    const html = document.createElement("div");
    html.innerHTML = renderToString(form);
    expect(
      html.querySelector('button[type="submit"]')?.hasAttribute("disabled"),
    ).toBe(true);
    expect(
      html
        .querySelector('input[name="expectedRevision"]')
        ?.getAttribute("value"),
    ).toBe("4");

    // The control: hydrated, the same form can be submitted.
    render(form);
    expect(screen.getByRole("button", { name: "Save settings" })).toBeEnabled();
  });
});
