import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ApiUnreachableReason } from "@/components/api-unreachable";

// The page tests assert that each site shows the notice; this asserts
// what the notice says, which is the part that used to be written three
// times and could drift once without any of them noticing.
describe("api unreachable reason", () => {
  it("names the missing data and rules out sign-in and permission", () => {
    render(
      <p>
        <ApiUnreachableReason subject="your organizations" />
      </p>,
    );

    expect(
      screen.getByText(
        "The garam API is not answering, so this page cannot show your organizations. This is not a sign-in problem and not a permission problem: try again once the API is back.",
      ),
    ).toBeInTheDocument();
  });
});
