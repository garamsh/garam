import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SecretReveal } from "@/components/secret-reveal";

// Restored by hand: `unstubAllGlobals` would also take the setup's
// `matchMedia` stub.
const OriginalURL = URL;

afterEach(() => {
  vi.stubGlobal("URL", OriginalURL);
  vi.restoreAllMocks();
});

const SECRET = "garam_sk_live_7f3a9c";

function renderReveal(onDone = vi.fn(), filename?: string) {
  render(
    <SecretReveal
      open
      title="Save the operator key"
      description="This key is shown once."
      secret={SECRET}
      noun="key"
      filename={filename}
      onDone={onDone}
    />,
  );
  return onDone;
}

describe("SecretReveal", () => {
  it("shows the secret in a dialog that names what it is", () => {
    renderReveal();

    const dialog = screen.getByRole("alertdialog", {
      name: "Save the operator key",
    });
    expect(dialog).toHaveTextContent(SECRET);
  });

  it("cannot be dismissed before the secret is kept", async () => {
    const user = userEvent.setup();
    const onDone = renderReveal();

    await user.keyboard("{Escape}");

    expect(onDone).not.toHaveBeenCalled();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("closes once the secret is copied", async () => {
    const user = userEvent.setup();
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue();
    const onDone = renderReveal();

    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith(SECRET);
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(onDone).toHaveBeenCalledOnce();
  });

  it("stays shut when the copy fails, and says how to keep it", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
      new Error("denied"),
    );
    renderReveal();

    await user.click(screen.getByRole("button", { name: "Copy" }));

    // Presence: the sentence goes on to say how to copy it by hand.
    expect(await screen.findByRole("alert")).toHaveTextContent(/Copy failed/);
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("closes once the secret is acknowledged as stored", async () => {
    const user = userEvent.setup();
    const onDone = renderReveal();

    // Presence: the label goes on to say it will not be shown again.
    await user.click(screen.getByRole("checkbox", { name: /stored the key/ }));
    await user.keyboard("{Escape}");

    expect(onDone).toHaveBeenCalledOnce();
  });

  it("downloads the secret as a file built in the browser", async () => {
    const user = userEvent.setup();
    // jsdom implements neither; both are restored after the test.
    const createObjectURL = vi.fn<(blob: Blob) => string>(() => "blob:local");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL = createObjectURL;
        static revokeObjectURL = revokeObjectURL;
      },
    );
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});
    renderReveal(vi.fn(), "operator.pem");

    await user.click(screen.getByRole("button", { name: "Download" }));

    const blob = createObjectURL.mock.calls[0]?.[0];
    expect(await blob?.text()).toBe(SECRET);
    expect(click).toHaveBeenCalledOnce();
    await vi.waitFor(() =>
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:local"),
    );
    expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
  });

  it("does not count a download that never reached the browser", async () => {
    const user = userEvent.setup();
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL = () => {
          throw new Error("blocked");
        };
        static revokeObjectURL = vi.fn();
      },
    );
    renderReveal(vi.fn(), "operator.pem");

    await user.click(screen.getByRole("button", { name: "Download" }));

    // Presence: the sentence goes on to offer copying instead.
    expect(screen.getByRole("alert")).toHaveTextContent(/Download failed/);
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("ignores a click outside before the secret is kept", async () => {
    const user = userEvent.setup();
    const onDone = renderReveal();

    // The overlay is what a click outside the dialog lands on.
    const overlay = document.querySelector(
      '[data-slot="alert-dialog-overlay"]',
    );
    expect(overlay).not.toBeNull();
    await user.click(overlay as Element);

    expect(onDone).not.toHaveBeenCalled();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });

  it("starts a second secret unkept, though the first was acknowledged", async () => {
    const user = userEvent.setup();
    const onDone = vi.fn();
    const props = {
      title: "Save the operator key",
      description: "This key is shown once.",
      noun: "key",
      onDone,
    };
    const { rerender } = render(
      <SecretReveal open secret={SECRET} {...props} />,
    );
    await user.click(screen.getByRole("checkbox", { name: /stored the key/ }));
    expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();

    rerender(<SecretReveal open={false} secret={SECRET} {...props} />);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    rerender(<SecretReveal open secret="garam_sk_live_second" {...props} />);

    expect(screen.getByRole("alertdialog")).toHaveTextContent(
      "garam_sk_live_second",
    );
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
    expect(
      screen.getByRole("checkbox", { name: /stored the key/ }),
    ).not.toBeChecked();
  });

  it("keeps nothing once closed, so reopening the same secret starts over", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    const props = {
      title: "Save the operator key",
      description: "This key is shown once.",
      secret: SECRET,
      noun: "key",
      onDone: vi.fn(),
    };
    const { rerender } = render(<SecretReveal open {...props} />);
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();

    rerender(<SecretReveal open={false} {...props} />);
    rerender(<SecretReveal open {...props} />);

    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("puts the secret in no URL and no console output", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    const logged = (["log", "info", "warn", "error", "debug"] as const).map(
      (method) => vi.spyOn(console, method),
    );
    const before = window.location.href;
    renderReveal();

    await user.click(screen.getByRole("button", { name: "Copy" }));
    await user.click(screen.getByRole("button", { name: "Done" }));

    expect(window.location.href).toBe(before);
    expect(window.location.href).not.toContain(SECRET);
    for (const spy of logged) {
      expect(JSON.stringify(spy.mock.calls)).not.toContain(SECRET);
    }
  });

  it("puts no name on the secret's pre, which ARIA prohibits", () => {
    renderReveal();

    const pre = screen.getByText(SECRET);
    expect(pre.tagName).toBe("PRE");
    expect(pre).not.toHaveAttribute("aria-label");
  });

  it("offers no download without a file name", () => {
    renderReveal();

    expect(
      screen.queryByRole("button", { name: "Download" }),
    ).not.toBeInTheDocument();
  });
});
