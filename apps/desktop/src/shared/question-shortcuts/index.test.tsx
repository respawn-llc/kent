import { fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { expect, it, vi } from "bun:test";
import { useQuestionNumberShortcuts } from "./index";

function Question({
  name,
  select,
  count = 7,
  neitherIndex = 6,
}: {
  name: string;
  select: (index: number) => void;
  count?: number;
  neitherIndex?: number;
}) {
  const questionRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<HTMLTextAreaElement>(null);
  useQuestionNumberShortcuts({
    questionRef,
    editorRef,
    optionCount: count,
    neitherIndex,
    enabled: true,
    onSelect: select,
  });
  return (
    <div ref={questionRef} data-testid={name} data-question-number-shortcuts>
      <textarea aria-label={name} ref={editorRef} />
    </div>
  );
}

function position(name: string, top: number) {
  vi.spyOn(screen.getByTestId(name), "getBoundingClientRect").mockReturnValue(new DOMRect(0, top, 300, 200));
}

it("selects the number on the first top-visible Question and focuses its commentary", () => {
  const earlier = vi.fn();
  const selected = vi.fn();
  const later = vi.fn();
  render(
    <>
      <Question name="clipped" select={earlier} />
      <Question name="first" select={selected} />
      <Question name="later" select={later} />
    </>,
  );
  position("clipped", -20);
  position("first", 20);
  position("later", 300);
  fireEvent.keyDown(document.body, { key: "6" });
  expect(selected).toHaveBeenCalledWith(5);
  expect(earlier).not.toHaveBeenCalled();
  expect(later).not.toHaveBeenCalled();
  expect(screen.getByLabelText("first")).toHaveFocus();
});

it.each(["0", "7"])("selects Neither using %s without submitting", (key) => {
  const select = vi.fn();
  render(<Question name="question" select={select} />);
  position("question", 20);
  fireEvent.keyDown(document.body, { key });
  expect(select).toHaveBeenCalledWith(6);
});

it("limits numbered shortcuts to 1–9 while 0 still selects Neither beyond nine", () => {
  const select = vi.fn();
  render(<Question name="question" select={select} count={12} neitherIndex={11} />);
  position("question", 20);
  fireEvent.keyDown(document.body, { key: "9" });
  expect(select).toHaveBeenLastCalledWith(8);
  screen.getByLabelText("question").blur();
  fireEvent.keyDown(document.body, { key: "0" });
  expect(select).toHaveBeenLastCalledWith(11);
});

it("does not capture digits while typing, modified keys, or repeated keydowns", () => {
  const select = vi.fn();
  render(
    <>
      <Question name="question" select={select} />
      <input aria-label="other" />
      <div contentEditable data-testid="editable" />
    </>,
  );
  position("question", 20);
  for (const target of [
    screen.getByLabelText("question"),
    screen.getByLabelText("other"),
    screen.getByTestId("editable"),
  ]) {
    target.focus();
    expect(fireEvent.keyDown(target, { key: "2" })).toBe(true);
    target.blur();
  }
  for (const modifiers of [
    { ctrlKey: true },
    { metaKey: true },
    { altKey: true },
    { shiftKey: true },
    { repeat: true },
    { isComposing: true },
  ]) {
    fireEvent.keyDown(document.body, { key: "2", ...modifiers });
  }
  expect(select).not.toHaveBeenCalled();
});
