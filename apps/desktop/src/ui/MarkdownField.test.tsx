import { render, screen } from "@testing-library/react";

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
  it("supports the exact pixel Goal clamp and floating action slot", () => {
    render(
      <CollapsibleMarkdownField
        {...baseProps}
        collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
        expanded={false}
        expandLabel="Expand"
        floatingAction={<button type="button">Save</button>}
      />,
    );

    expect(screen.getByTestId("markdown-field-read-content-viewport")).toHaveStyle({
      maxHeight: "300px",
    });
    expect(screen.getByText("Save")).toBeInTheDocument();
  });

  it("keeps the existing line clamp as an explicit default choice", () => {
    render(
      <CollapsibleMarkdownField
        {...baseProps}
        collapsedHeightClamp={{ kind: "lines", maximumLines: 10, minimumLines: 5, viewportPercent: 50 }}
        expanded={false}
        expandLabel="Expand"
      />,
    );

    expect(screen.getByTestId("markdown-field-read-content-viewport")).toHaveStyle({
      maxHeight: "clamp(5lh,50dvh,10lh)",
    });
  });
});
