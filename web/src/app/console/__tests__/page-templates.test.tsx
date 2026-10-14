import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { DetailPage } from "@/app/console/_components/templates/detail-page";
import { ListPage } from "@/app/console/_components/templates/list-page";
import { SettingsPage } from "@/app/console/_components/templates/settings-page";
import { WizardPage } from "@/app/console/_components/templates/wizard-page";
import { StatusDot } from "@/components/status-dot";

describe("ListPage", () => {
  it("heads the list with its title, count and primary action", () => {
    render(
      <ListPage
        title="Agents"
        description="Every agent this organization runs."
        count={12}
        primaryAction={<button type="button">New agent</button>}
        filters={<input aria-label="Search agents" />}
      >
        <table aria-label="Agents" />
      </ListPage>,
    );

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Agents12",
    );
    expect(
      screen.getByRole("button", { name: "New agent" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Search agents" }),
    ).toBeInTheDocument();
  });

  it("guesses no count for a list it could not read", () => {
    render(
      <ListPage title="Agents" description="Every agent.">
        <p>Not checked</p>
      </ListPage>,
    );

    expect(
      screen.getByRole("heading", { level: 1, name: "Agents" }),
    ).toBeInTheDocument();
  });
});

describe("DetailPage", () => {
  const tabs = [
    { value: "overview", label: "Overview", content: <p>Facts</p> },
    { value: "activity", label: "Activity", content: <p>Events</p> },
    { value: "settings", label: "Settings", content: <p>Forms</p> },
  ];

  it("titles the page with the name and offers the id to copy", () => {
    render(
      <DetailPage
        name="Field one"
        idLabel="Operator ID"
        id="field-one"
        status={<StatusDot tone="ok" label="Enrolled" />}
        tabs={tabs}
      />,
    );

    expect(
      screen.getByRole("heading", { level: 1, name: "Field one" }),
    ).toBeInTheDocument();
    expect(screen.getByText("field-one")).toHaveAccessibleName("Operator ID");
    expect(screen.getByText("Enrolled")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Copy operator id" }),
    ).toBeInTheDocument();
  });

  it("closes the last tab with the danger zone, and only that tab", async () => {
    const user = userEvent.setup();
    render(
      <DetailPage
        name="Field one"
        idLabel="Operator ID"
        id="field-one"
        tabs={tabs}
        dangerZone={[
          {
            title: "Delete operator",
            description: "Work already running in its cluster does not stop.",
            control: <button type="button">Delete operator</button>,
          },
        ]}
      />,
    );

    expect(screen.getByRole("tabpanel")).toHaveTextContent("Facts");
    expect(screen.queryByText("Danger zone")).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Settings" }));

    const panel = screen.getByRole("tabpanel");
    const zone = within(panel).getByRole("region", { name: "Danger zone" });
    expect(zone).toHaveTextContent(
      "Work already running in its cluster does not stop.",
    );
    expect(
      within(zone).getByRole("button", { name: "Delete operator" }),
    ).toBeInTheDocument();
  });
});

describe("SettingsPage", () => {
  it("gives each section its own heading and link", () => {
    render(
      <SettingsPage
        title="Settings"
        description="How this organization is named and reached."
        sections={[
          {
            id: "general",
            title: "General",
            description: "The name everyone sees.",
            content: <button type="submit">Save name</button>,
          },
          {
            id: "domain",
            title: "Domain",
            description: "Where people sign in.",
            content: <button type="submit">Save domain</button>,
          },
        ]}
      />,
    );

    const general = screen.getByRole("region", { name: "General" });
    expect(
      within(general).getByRole("button", { name: "Save name" }),
    ).toBeInTheDocument();
    expect(
      within(general).queryByRole("button", { name: "Save domain" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Domain" })).toHaveAttribute(
      "href",
      "#domain",
    );
  });
});

describe("WizardPage", () => {
  it("shows the steps beside the step in hand", () => {
    render(
      <WizardPage
        title="Connect an operator"
        description="Register it, issue its keys, and verify."
        steps={[
          { id: "register", title: "Register" },
          { id: "keys", title: "Issue keys" },
          { id: "verify", title: "Verify" },
        ]}
        current="keys"
        completed={["register"]}
      >
        <p>Issue a key for the manager.</p>
      </WizardPage>,
    );

    expect(
      screen.getByRole("list", { name: "Connect an operator steps" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "Issue keys" }),
    ).toHaveTextContent("Issue a key for the manager.");
  });
});
