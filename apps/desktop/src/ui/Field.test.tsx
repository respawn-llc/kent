import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";

import { TextInput } from "./Field";
import { SelectField } from "./SelectField";

describe("Field", () => {
  it("uses spaced overline labels globally", () => {
    render(<TextInput label="Title" value="" readOnly />);

    expect(screen.getByText("Title")).toHaveClass("tracking-[0.1em]");
  });

  it("renders SelectField without native select markup", () => {
    const onValueChange = vi.fn();

    render(
      <SelectField
        label="Source"
        onValueChange={onValueChange}
        options={[
          { label: "Main", value: "workspace-1" },
          { label: "Docs", value: "workspace-2" },
        ]}
        value="workspace-1"
      />,
    );

    const trigger = screen.getByRole("combobox", { name: "Source" });
    expect(trigger).toHaveAttribute("data-slot", "select-trigger");
    expect(trigger).toHaveAttribute("type", "button");
    expect(trigger).toHaveTextContent("Main");

    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole("option", { name: "Docs" }));

    expect(onValueChange).toHaveBeenCalledWith("workspace-2");
  });
});
