import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Time } from "@/components/time";
import { TooltipProvider } from "@garam/design/ui/tooltip";

// jsdom has no ResizeObserver; the tooltip's positioning asks for one.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

beforeEach(() => {
  vi.useFakeTimers({ now: new Date("2026-10-14T09:00:00Z"), toFake: ["Date"] });
});

afterEach(() => {
  vi.useRealTimers();
});

function renderTime(value: string) {
  return render(
    <TooltipProvider>
      <Time value={value} />
    </TooltipProvider>,
  );
}

describe("Time", () => {
  it("shows the instant relative to now, with the instant in dateTime", () => {
    renderTime("2026-10-14T08:57:00Z");

    const time = screen.getByText("3 minutes ago");
    expect(time.tagName).toBe("TIME");
    expect(time).toHaveAttribute("dateTime", "2026-10-14T08:57:00.000Z");
  });

  it("gives the exact time with its zone on focus", async () => {
    const user = userEvent.setup();
    renderTime("2026-10-14T08:57:00Z");

    await user.tab();

    // Presence: the zone the runtime reports closes the sentence.
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      /Oct 14, 2026, \d\d:57:00/,
    );
  });

  it("says the time is unknown rather than show what it was given", () => {
    renderTime("yesterday-ish");

    expect(screen.getByText("Unknown time")).toBeInTheDocument();
    expect(screen.queryByText("yesterday-ish")).not.toBeInTheDocument();
  });
});
