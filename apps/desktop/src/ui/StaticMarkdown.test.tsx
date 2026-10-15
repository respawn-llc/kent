import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";
import { Streamdown } from "streamdown";

import { StaticMarkdown } from "./MarkdownText";

it("parses each static Markdown value once without a hydration reparse", () => {
  const parsed = vi.fn();
  const plugins = [() => parsed];
  const content = (value: string) => (
    <Streamdown mode="static" remarkPlugins={plugins}>
      {value}
    </Streamdown>
  );
  const view = render(content("First **message**"));
  expect(parsed).toHaveBeenCalledTimes(1);

  view.rerender(content("First **message**"));
  expect(parsed).toHaveBeenCalledTimes(1);

  view.rerender(content("Second **message**"));
  expect(parsed).toHaveBeenCalledTimes(2);
});

describe("StaticMarkdown task lists", () => {
  it("replaces an editable checklist without using stale source offsets", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup({ delay: null });
    const initialValue = "- [ ] Keep the Markdown source";
    const updatedValue = "New context\n\n- [ ] Keep the Markdown source";
    const view = render(
      <StaticMarkdown
        onTaskListChange={onChange}
        taskListItemToggleLabel={() => "Toggle item"}
        value={initialValue}
      />,
    );

    view.rerender(
      <StaticMarkdown
        onTaskListChange={onChange}
        taskListItemToggleLabel={() => "Toggle item"}
        value={updatedValue}
      />,
    );
    await user.click(screen.getByRole("checkbox"));

    expect(onChange).toHaveBeenCalledWith("New context\n\n- [x] Keep the Markdown source");
  });

  it("toggles the selected task marker without rewriting surrounding Markdown", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup({ delay: null });
    const value = "Before\n\n3. [X] **Keep this formatting**\n\nAfter";

    render(
      <StaticMarkdown
        onTaskListChange={onChange}
        taskListItemToggleLabel={() => "Toggle item"}
        value={value}
      />,
    );

    await user.click(screen.getByRole("checkbox"));

    expect(onChange).toHaveBeenCalledWith("Before\n\n3. [ ] **Keep this formatting**\n\nAfter");
  });
});
