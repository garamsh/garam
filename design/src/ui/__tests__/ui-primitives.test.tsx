import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "../alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "../dialog";
import { Kbd } from "../kbd";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../tabs";

// The shadcn primitives the foundation adds are CLI output over Radix;
// these pin the behaviour the console relies on from each.

describe("Select", () => {
  it("submits the chosen value under its name", async () => {
    const user = userEvent.setup();
    const submitted = vi.fn();
    render(
      <form
        onSubmit={(event) => {
          event.preventDefault();
          submitted(new FormData(event.currentTarget).get("role"));
        }}
      >
        <Select name="role" defaultValue="member">
          <SelectTrigger aria-label="Role">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="member">Member</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
          </SelectContent>
        </Select>
        <button type="submit">Invite</button>
      </form>,
    );

    await user.click(screen.getByRole("combobox", { name: "Role" }));
    await user.click(await screen.findByRole("option", { name: "Admin" }));
    await user.click(screen.getByRole("button", { name: "Invite" }));

    expect(submitted).toHaveBeenCalledWith("admin");
  });
});

describe("Tabs", () => {
  it("moves between panels from the keyboard", async () => {
    const user = userEvent.setup();
    render(
      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">Facts</TabsContent>
        <TabsContent value="settings">Forms</TabsContent>
      </Tabs>,
    );

    await user.click(screen.getByRole("tab", { name: "Overview" }));
    await user.keyboard("{ArrowRight}");

    expect(screen.getByRole("tab", { name: "Settings" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("tabpanel")).toHaveTextContent("Forms");
  });
});

describe("Dialog", () => {
  it("is named by its title and closes on Escape", async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger>Edit</DialogTrigger>
        <DialogContent>
          <DialogTitle>Rename team</DialogTitle>
          <DialogDescription>Everyone sees the new name.</DialogDescription>
        </DialogContent>
      </Dialog>,
    );

    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(
      screen.getByRole("dialog", { name: "Rename team" }),
    ).toBeInTheDocument();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});

describe("AlertDialog", () => {
  it("asks with the act's own label and runs it only when confirmed", async () => {
    const user = userEvent.setup();
    const revoke = vi.fn();
    render(
      <AlertDialog>
        <AlertDialogTrigger>Revoke key</AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogTitle>Revoke this key?</AlertDialogTitle>
          <AlertDialogDescription>
            Tokens it issued stop working.
          </AlertDialogDescription>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={revoke}>Revoke key</AlertDialogAction>
        </AlertDialogContent>
      </AlertDialog>,
    );

    await user.click(screen.getByRole("button", { name: "Revoke key" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(revoke).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Revoke key" }));
    const dialog = screen.getByRole("alertdialog", {
      name: "Revoke this key?",
    });
    expect(dialog).toBeInTheDocument();
    await user.click(
      screen.getAllByRole("button", { name: "Revoke key" }).at(-1)!,
    );
    expect(revoke).toHaveBeenCalledOnce();
  });
});

describe("Kbd", () => {
  it("renders a key as a kbd element", () => {
    render(<Kbd>⌘K</Kbd>);

    expect(screen.getByText("⌘K").tagName).toBe("KBD");
  });
});
