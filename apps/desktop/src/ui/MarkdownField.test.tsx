import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";

import { CollapsibleMarkdownField } from "./MarkdownField";

const baseProps = {
  disabled: false,
  editorMinHeight: 120,
  editing: false,
  label: "Description",
  onChange: vi.fn(),
  onEdit: vi.fn(),
  onEditingChange: vi.fn(),
  onExpand: vi.fn(),
  placeholder: "placeholder",
  value: "A description",
};

describe("MarkdownField presentation options", () => {
  it("keeps the editor mounted while pressing its action and exits when focus leaves the field", async () => {
    const save = vi.fn();
    function Example() {
      const [editing, setEditing] = useState(true);
      return (
        <>
          <CollapsibleMarkdownField
            {...baseProps}
            editing={editing}
            onEditingChange={setEditing}
            onEdit={() => {
              setEditing(true);
            }}
            expanded
            expandLabel="Expand"
            collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
            floatingAction={<button onClick={save}>Save</button>}
          />
          <button>Outside</button>
        </>
      );
    }
    render(<Example />);
    const user = userEvent.setup();
    const editor = screen.getByRole("textbox", { name: "Description" });
    const button = screen.getByRole("button", { name: "Save" });
    await user.pointer({ target: button, keys: "[MouseLeft>]" });
    expect(editor).toBeInTheDocument();
    expect(save).not.toHaveBeenCalled();
    await user.pointer({ target: button, keys: "[/MouseLeft]" });
    expect(save).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Outside" }));
    expect(editor).not.toBeInTheDocument();
  });

  it("keeps only the latest floating action as an inert exit presentation", () => {
    const firstAction = vi.fn();
    const latestAction = vi.fn();
    const view = render(
      <CollapsibleMarkdownField
        {...baseProps}
        collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
        expanded={false}
        expandLabel="Expand"
        floatingAction={
          <button onClick={firstAction} type="button">
            First Save
          </button>
        }
      />,
    );

    view.rerender(
      <CollapsibleMarkdownField
        {...baseProps}
        collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
        expanded={false}
        expandLabel="Expand"
        floatingAction={
          <button onClick={latestAction} type="button">
            Latest Save
          </button>
        }
      />,
    );
    expect(screen.getByRole("button", { name: "Latest Save" })).toBeInTheDocument();

    const latestButton = screen.getByRole("button", { name: "Latest Save" });
    latestButton.focus();
    view.rerender(
      <CollapsibleMarkdownField
        {...baseProps}
        collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
        expanded={false}
        expandLabel="Expand"
      />,
    );

    const retainedButton = screen.getByRole("button", {
      hidden: true,
      name: "Latest Save",
    });
    expect(retainedButton).toBeInTheDocument();
    expect(retainedButton).not.toHaveFocus();

    fireEvent.click(retainedButton);
    fireEvent.keyDown(retainedButton, { key: "Enter" });
    expect(firstAction).not.toHaveBeenCalled();
    expect(latestAction).not.toHaveBeenCalled();
  });
});
