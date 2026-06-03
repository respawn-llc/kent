import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";

import { SelectField } from "./SelectField";

describe("Field", () => {
  it("renders SelectField through a dropdown portal without native select markup", async () => {
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

    const trigger = screen.getByRole("button", { name: "Source" });
    expect(trigger).toHaveAttribute("type", "button");

    fireEvent.pointerDown(trigger);
    const menu = await screen.findByRole("menu");
    expect(document.body).toContainElement(menu);

    fireEvent.click(screen.getByRole("menuitemradio", { name: "Docs" }));

    expect(onValueChange).toHaveBeenCalledWith("workspace-2");
  });

  it("closes an open SelectField menu when the field becomes disabled", async () => {
    const onValueChange = vi.fn();
    const options = [
      { label: "Main", value: "workspace-1" },
      { label: "Docs", value: "workspace-2" },
    ];

    const { rerender } = render(
      <SelectField
        label="Source"
        onValueChange={onValueChange}
        options={options}
        value="workspace-1"
      />,
    );

    fireEvent.pointerDown(screen.getByRole("button", { name: "Source" }));
    expect(await screen.findByRole("menu")).toBeInTheDocument();

    rerender(
      <SelectField
        disabled
        label="Source"
        onValueChange={onValueChange}
        options={options}
        value="workspace-1"
      />,
    );

    expect(screen.getByRole("button", { name: "Source" })).toBeDisabled();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(onValueChange).not.toHaveBeenCalled();
  });

  it("does not open a disabled SelectField", () => {
    const onValueChange = vi.fn();
    const submitted = vi.fn();

    render(
      <form
        onSubmit={(event) => {
          event.preventDefault();
          submitted(Object.fromEntries(new FormData(event.currentTarget)));
        }}
      >
        <SelectField
          disabled
          label="Source"
          name="source"
          onValueChange={onValueChange}
          options={[
            { label: "Main", value: "workspace-1" },
            { label: "Docs", value: "workspace-2" },
          ]}
          value="workspace-1"
        />
        <button type="submit">Submit</button>
      </form>,
    );

    fireEvent.pointerDown(screen.getByRole("button", { name: "Source" }));
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));

    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(submitted).toHaveBeenCalledWith({});
    expect(onValueChange).not.toHaveBeenCalled();
  });
});
