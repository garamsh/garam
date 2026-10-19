import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ApiUnreachableReason } from "@/components/api-unreachable";

// The page tests assert that each site shows the notice; this asserts
// what the notice says, which is the part that used to be written three
// times and could drift once without any of them noticing.
describe("api unreachable reason", () => {
  it("names the missing data and says to try again", () => {
    render(
      <p>
        <ApiUnreachableReason subject="your organizations" />
      </p>,
    );

    expect(
      screen.getByText(
        "This page can't show your organizations until garam answers. Try again shortly.",
      ),
    ).toBeInTheDocument();
  });
});
