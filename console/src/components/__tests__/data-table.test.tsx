import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { DataTable, type ColumnDef } from "@/components/data-table";

type Row = { id: string; name: string; state: string };

const columns: ColumnDef<Row, unknown>[] = [
  { accessorKey: "name", header: "Name" },
  { accessorKey: "state", header: "State", enableSorting: false },
];

const rows: Row[] = [
  { id: "b", name: "Beta", state: "Ready" },
  { id: "a", name: "Alpha", state: "Failed" },
  { id: "c", name: "Gamma", state: "Ready" },
];

function names() {
  return screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell")[0]?.textContent);
}

describe("DataTable", () => {
  it("lists the rows under a caption naming the table", () => {
    render(
      <DataTable label="Agents" columns={columns} data={rows} empty="None" />,
    );

    expect(screen.getByRole("table", { name: "Agents" })).toBeInTheDocument();
    expect(names()).toEqual(["Beta", "Alpha", "Gamma"]);
  });

  it("sorts by a sortable column and says which way", async () => {
    const user = userEvent.setup();
    render(
      <DataTable label="Agents" columns={columns} data={rows} empty="None" />,
    );

    await user.click(screen.getByRole("button", { name: "Name" }));

    expect(names()).toEqual(["Alpha", "Beta", "Gamma"]);
    expect(screen.getByRole("columnheader", { name: "Name" })).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
    // Control: a column that opts out offers no sort control.
    expect(
      screen.queryByRole("button", { name: "State" }),
    ).not.toBeInTheDocument();
  });

  it("narrows the rows to a search", async () => {
    const user = userEvent.setup();
    render(
      <DataTable
        label="Agents"
        columns={columns}
        data={rows}
        empty="None"
        search={{ placeholder: "Search agents" }}
      />,
    );

    await user.type(
      screen.getByRole("searchbox", { name: "Search agents" }),
      "fail",
    );

    expect(names()).toEqual(["Alpha"]);
  });

  it("says when a search matches nothing, rather than showing the empty state", async () => {
    const user = userEvent.setup();
    render(
      <DataTable
        label="Agents"
        columns={columns}
        data={rows}
        empty="No agents yet"
        search={{ placeholder: "Search agents" }}
      />,
    );

    await user.type(
      screen.getByRole("searchbox", { name: "Search agents" }),
      "zeta",
    );

    expect(screen.getByText("Nothing matches “zeta”.")).toBeInTheDocument();
    expect(screen.queryByText("No agents yet")).not.toBeInTheDocument();
  });

  it("shows the empty state for a list with nothing in it", () => {
    render(
      <DataTable
        label="Agents"
        columns={columns}
        data={[]}
        empty="No agents yet"
      />,
    );

    expect(screen.getByText("No agents yet")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("pages a list longer than a page", async () => {
    const user = userEvent.setup();
    const many = Array.from({ length: 5 }, (_, index) => ({
      id: String(index),
      name: `Agent ${index + 1}`,
      state: "Ready",
    }));
    render(
      <DataTable
        label="Agents"
        columns={columns}
        data={many}
        empty="None"
        pageSize={2}
      />,
    );

    expect(names()).toEqual(["Agent 1", "Agent 2"]);
    expect(screen.getByText("1–2 of 5")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Next" }));

    expect(names()).toEqual(["Agent 3", "Agent 4"]);
    expect(screen.getByText("3–4 of 5")).toBeInTheDocument();
  });
});
