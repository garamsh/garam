import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { DescriptionList } from "@/components/description-list";

describe("DescriptionList", () => {
  it("pairs each term with its description", () => {
    render(
      <DescriptionList
        items={[
          { term: "Name", description: "Build runners" },
          { term: "Created", description: "3 days ago" },
        ]}
      />,
    );

    const terms = screen.getAllByRole("term").map((term) => term.textContent);
    const descriptions = screen
      .getAllByRole("definition")
      .map((definition) => definition.textContent);
    expect(terms).toEqual(["Name", "Created"]);
    expect(descriptions).toEqual(["Build runners", "3 days ago"]);
  });
});
