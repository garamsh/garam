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
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Could not copy the key/,
    );
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
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:local");
    expect(screen.getByRole("button", { name: "Done" })).toBeEnabled();
  });

  it("offers no download without a file name", () => {
    renderReveal();

    expect(
      screen.queryByRole("button", { name: "Download" }),
    ).not.toBeInTheDocument();
  });
});
