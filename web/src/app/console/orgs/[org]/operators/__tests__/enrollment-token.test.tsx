import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { EnrollmentToken } from "@/app/console/orgs/[org]/operators/_components/enrollment-token";

function panel(token: string) {
  return (
    <EnrollmentToken title="New enrollment token" token={token}>
      <p>Instructions</p>
    </EnrollmentToken>
  );
}

/** A denied clipboard permission, as the browser answers one. */
function denyClipboard() {
  vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
    new DOMException("Write permission denied.", "NotAllowedError"),
  );
}

describe("enrollment token", () => {
  it("says the copy failed and leaves the token in the field to copy by hand", async () => {
    const user = userEvent.setup();
    render(panel("first-token"));

    // The control for the refusal: the same button with the clipboard
    // answering. Without it a missing "Copied" is as much a handler
    // that never ran as one that was refused.
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(
      await screen.findByRole("button", { name: "Copied" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    denyClipboard();
    await user.click(screen.getByRole("button", { name: "Copied" }));

    expect(
      await screen.findByText(
        "Could not copy the token. Select it in the field and copy it yourself.",
      ),
    ).toHaveAttribute("role", "alert");
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
    expect(screen.getByLabelText("Enrollment token")).toHaveValue(
      "first-token",
    );
  });

  it("carries neither the copied claim nor the failure over to a different token", async () => {
    const user = userEvent.setup();
    const { rerender } = render(panel("first-token"));

    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(
      await screen.findByRole("button", { name: "Copied" }),
    ).toBeInTheDocument();

    // The control: the same token re-rendered keeps its claim, so the
    // reset below is the token changing and not the re-render.
    rerender(panel("first-token"));
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();

    rerender(panel("second-token"));
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();

    denyClipboard();
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();

    rerender(panel("third-token"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Enrollment token")).toHaveValue(
      "third-token",
    );
  });
});
