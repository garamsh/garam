import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
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
    expect(screen.getByRole("group", { name: "Operator ID" })).toContainElement(
      screen.getByText("field-one"),
    );
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

    const overview = screen.getByRole("tabpanel", { name: "Overview" });
    expect(overview).toHaveTextContent("Facts");
    expect(
      within(overview).queryByRole("region", { name: "Danger zone" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Settings" }));

    const panel = screen.getByRole("tabpanel", { name: "Settings" });
    const zone = within(panel).getByRole("region", { name: "Danger zone" });
    expect(zone).toHaveTextContent(
      "Work already running in its cluster does not stop.",
    );
    expect(
      within(zone).getByRole("button", { name: "Delete operator" }),
    ).toBeInTheDocument();
  });
});

describe("DetailPage tabs", () => {
  /** A form holding typed input and the outcome of its last submit. */
  function RenameForm() {
    const [outcome, setOutcome] = useState("");
    return (
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const name = new FormData(event.currentTarget).get("name");
          setOutcome(name === "taken" ? "That name is taken." : "Saved.");
        }}
      >
        <input name="name" aria-label="Name" />
        <button type="submit">Save</button>
        {outcome && <p role="status">{outcome}</p>}
      </form>
    );
  }

  function renderPage() {
    render(
      <DetailPage
        name="Field one"
        idLabel="Operator ID"
        id="field-one"
        tabs={[
          { value: "overview", label: "Overview", content: <p>Facts</p> },
          { value: "settings", label: "Settings", content: <RenameForm /> },
        ]}
      />,
    );
  }

  async function switchAwayAndBack(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("tab", { name: "Overview" }));
    // The switch happened: Settings is no longer the tab in view. Read
    // off the tabs, which stand whether or not a panel stays mounted.
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("tab", { name: "Settings" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
    await user.click(screen.getByRole("tab", { name: "Settings" }));
  }

  it("keeps typed input and a refusal across a tab switch", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(screen.getByRole("tab", { name: "Settings" }));
    await user.type(screen.getByRole("textbox", { name: "Name" }), "taken");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("status")).toHaveTextContent("That name is taken.");

    await switchAwayAndBack(user);

    expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue("taken");
    expect(screen.getByRole("status")).toHaveTextContent("That name is taken.");
  });

  it("keeps an inline success across a tab switch", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(screen.getByRole("tab", { name: "Settings" }));
    await user.type(screen.getByRole("textbox", { name: "Name" }), "Field");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await switchAwayAndBack(user);

    expect(screen.getByRole("status")).toHaveTextContent("Saved.");
  });

  it("keeps each panel a tabpanel named by its tab", () => {
    renderPage();

    for (const name of ["Overview", "Settings"]) {
      const tab = screen.getByRole("tab", { name });
      expect(screen.getByRole("tabpanel", { name })).toHaveAttribute(
        "aria-labelledby",
        tab.id,
      );
    }
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
