import userEvent from "@testing-library/user-event";
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
  it("renders a supplied floating action in read presentation", () => {
    render(
      <CollapsibleMarkdownField
        {...baseProps}
        collapsedHeightClamp={{ kind: "pixels", maximumPixels: 300 }}
        expanded={false}
        expandLabel="Expand"
        floatingAction={<button type="button">Save</button>}
      />,
    );

    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("invokes the supplied submit intent for the configured shortcut", async () => {
    const onSubmitIntent = vi.fn();
    render(
      <CollapsibleMarkdownField
        {...baseProps}
        collapsedHeightClamp={{ kind: "lines", maximumLines: 10, minimumLines: 5, viewportPercent: 50 }}
        editing
        expanded={false}
        expandLabel="Expand"
        submitIntent={{ available: true, onSubmitIntent, policy: "meta-enter" }}
      />,
    );

    await userEvent.setup().click(screen.getByRole("textbox", { name: "Description" }));
    await userEvent.setup().keyboard("{Meta>}{Enter}{/Meta}");

    expect(onSubmitIntent).toHaveBeenCalledOnce();
  });
});
