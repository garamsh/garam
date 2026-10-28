import { render, screen } from "@testing-library/react";
import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { useHydrated } from "@/hooks/use-hydrated";

function Field() {
  const hydrated = useHydrated();
  return <textarea aria-label="Editor" disabled={!hydrated} />;
}

describe("useHydrated", () => {
  it("keeps a field disabled in the server render and enables it in the browser", () => {
    expect(renderToString(<Field />)).toContain("disabled");

    // The control: rendered by React in the browser, the same field
    // takes input.
    render(<Field />);
    expect(screen.getByLabelText("Editor")).toBeEnabled();
  });
});
