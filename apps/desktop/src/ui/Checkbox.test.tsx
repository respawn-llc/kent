import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { Checkbox } from "./index";

describe("Checkbox", () => {
  it("renders the shadcn checkbox primitive", () => {
    const onCheckedChange = vi.fn();

    render(
      <div>
        <Checkbox id="requires-approval" onCheckedChange={onCheckedChange} />
        <label htmlFor="requires-approval">Requires approval</label>
      </div>,
    );

    const checkbox = screen.getByRole("checkbox", { name: "Requires approval" });

    expect(checkbox).not.toBeChecked();
  });

  it("associates labels and toggles from keyboard input", async () => {
    const user = userEvent.setup();
    const onCheckedChange = vi.fn();

    render(
      <div>
        <Checkbox id="requires-approval-keyboard" onCheckedChange={onCheckedChange} />
        <label htmlFor="requires-approval-keyboard">Requires approval</label>
      </div>,
    );

    const checkbox = screen.getByRole("checkbox", { name: "Requires approval" });

    await user.click(screen.getByText("Requires approval"));

    expect(checkbox).toBeChecked();
    expect(onCheckedChange).toHaveBeenLastCalledWith(true);

    checkbox.focus();
    await user.keyboard("[Space]");

    expect(checkbox).not.toBeChecked();
    expect(onCheckedChange).toHaveBeenLastCalledWith(false);
  });
});
