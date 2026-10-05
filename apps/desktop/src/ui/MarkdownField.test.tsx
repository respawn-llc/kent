import { fireEvent, render, screen } from "@testing-library/react";

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
