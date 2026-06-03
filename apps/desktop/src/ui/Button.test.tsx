import { render, screen } from "@testing-library/react";

import { Button } from "./Button";

describe("Button", () => {
  it("defaults to a native button action", () => {
    render(<Button variant="danger">Delete</Button>);

    expect(screen.getByRole("button", { name: "Delete" })).toHaveAttribute("type", "button");
  });
});
