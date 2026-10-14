import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { PolicyEditor } from "@/app/console/orgs/[org]/permissions/_components/policy-editor";
import { enter } from "@/testing/enter";
import type { DocumentProblem } from "@/lib/policy-document";

const DOCUMENT = JSON.stringify(
  {
    Version: "2026-07-25",
    Statement: [
      { Effect: "Deny", Action: ["project:delete"], Resource: ["*"] },
      { Effect: "Allow", Action: ["project:read"], Resource: ["*"] },
    ],
  },
  null,
  2,
);

/** The editor holding its text, which it shows after every change. */
function Harness({
  initial = DOCUMENT,
  problem = null,
}: {
  initial?: string;
  problem?: DocumentProblem | null;
}) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <PolicyEditor value={value} onChange={setValue} problem={problem} />
      <output aria-label="Sent">{value}</output>
    </>
  );
}

function sent(): unknown {
  return JSON.parse(
    screen.getByRole("status", { name: "Sent" }).textContent ?? "",
  );
}

describe("policy editor", () => {
  it("edits the statements and the JSON it sends as one document", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    const second = screen.getByRole("listitem", { name: "Statement 2" });
    const action = within(second).getByRole("textbox", {
      name: "Statement 2 action 1",
    });
    await user.clear(action);
    await enter(user, action, "project:list");
    await user.click(
      within(second).getByRole("button", { name: "Add resource" }),
    );

    expect(sent()).toEqual({
      Version: "2026-07-25",
      Statement: [
        { Effect: "Deny", Action: ["project:delete"], Resource: ["*"] },
        { Effect: "Allow", Action: ["project:list"], Resource: ["*", ""] },
      ],
    });

    await user.click(
      screen.getByRole("button", { name: "Remove statement 1" }),
    );
    expect(sent()).toMatchObject({
      Statement: [{ Effect: "Allow", Action: ["project:list"] }],
    });
  });

  it("marks the control garam's refusal names, and says it once", () => {
    render(
      <Harness
        problem={{
          message: "Statement 2's Effect must be Allow or Deny.",
          path: "/Statement/1/Effect",
        }}
      />,
    );

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "Statement 2's Effect must be Allow or Deny.",
    );
    const effect = screen.getByRole("combobox", { name: "Statement 2 effect" });
    expect(effect).toHaveAttribute("aria-invalid", "true");
    expect(effect).toHaveAccessibleDescription(
      "Statement 2's Effect must be Allow or Deny.",
    );
    // The controls: the same key in the other statement, and another
    // key of the same statement, are not marked.
    expect(
      screen.getByRole("combobox", { name: "Statement 1 effect" }),
    ).not.toHaveAttribute("aria-invalid");
    expect(
      screen.getByRole("textbox", { name: "Statement 2 label" }),
    ).not.toHaveAttribute("aria-invalid");
  });

  it("says a refusal with no place under the editor and marks no control", () => {
    render(
      <Harness problem={{ message: "This policy document can't be saved." }} />,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "This policy document can't be saved.",
    );
    for (const effect of screen.getAllByRole("combobox")) {
      expect(effect).not.toHaveAttribute("aria-invalid");
    }
  });

  it("sends JSON that will not parse to the JSON view and says why", () => {
    render(<Harness initial="{ oops" />);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "The document is not valid JSON.",
    );
    expect(screen.getByRole("tab", { name: "JSON" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(
      screen.queryByRole("list", { name: "Statements" }),
    ).not.toBeInTheDocument();
  });

  it("shows a key cased otherwise as the engine reads it, and edits it in place", async () => {
    const user = userEvent.setup();
    render(
      <Harness
        initial={JSON.stringify({
          Version: "2026-07-25",
          statement: [{ effect: "Deny", action: ["project:delete"] }],
        })}
      />,
    );

    const action = screen.getByRole("textbox", {
      name: "Statement 1 action 1",
    });
    expect(action).toHaveValue("project:delete");
    await user.clear(action);
    await enter(user, action, "project:update");

    // The edit lands under the key as written; no second spelling.
    expect(sent()).toEqual({
      Version: "2026-07-25",
      statement: [{ effect: "Deny", action: ["project:update"], Resource: [] }],
    });
  });
});
