import { render, screen } from "@testing-library/react";

import { Button } from "./Button";

describe("Button", () => {
  it("uses error outline and inherited icon color for danger actions", () => {
    render(
      <Button variant="danger">
        <svg aria-hidden="true" data-testid="danger-icon" stroke="currentColor" />
        Delete
      </Button>,
    );

    expect(screen.getByRole("button", { name: "Delete" })).toHaveClass(
      "border-[var(--color-error)]",
      "text-[var(--color-error)]",
    );
    expect(screen.getByTestId("danger-icon")).toHaveAttribute("stroke", "currentColor");
  });
});
