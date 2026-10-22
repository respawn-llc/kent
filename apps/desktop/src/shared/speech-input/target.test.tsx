import { expect, it } from "bun:test";
import { act, render, screen } from "@testing-library/react";
import { useRef, useState } from "react";
import { createSpeechTarget } from "./insertion";

it("delivers to the current draft and preserves a noncollapsed mounted-editor selection", () => {
  let draft = "old draft";
  const editor = document.createElement("textarea");
  editor.value = "typed during speech";
  editor.setSelectionRange(2, 7, "backward");
  const target = createSpeechTarget({
    read: () => draft,
    write: (text) => {
      draft = text;
      editor.value = text;
    },
    editor: () => editor,
  });
  draft = editor.value;
  expect(target.append("مرحبا")).toBe(true);
  expect(draft).toBe("typed during speech\n\nمرحبا");
  expect([editor.selectionStart, editor.selectionEnd, editor.selectionDirection]).toEqual([2, 7, "backward"]);
});

it("preserves the current selection through the React draft update", () => {
  let deliver!: (text: string) => boolean;
  function Editor() {
    const [text, setText] = useState("existing draft");
    const editor = useRef<HTMLTextAreaElement>(null);
    deliver = createSpeechTarget({ read: () => text, write: setText, editor: () => editor.current }).append;
    return (
      <textarea
        ref={editor}
        value={text}
        onChange={(event) => {
          setText(event.target.value);
        }}
      />
    );
  }
  render(<Editor />);
  const editor = screen.getByRole<HTMLTextAreaElement>("textbox");
  editor.setSelectionRange(1, 5);
  act(() => {
    deliver("new speech");
  });
  expect(editor.value).toBe("existing draft\n\nnew speech");
  expect([editor.selectionStart, editor.selectionEnd]).toEqual([1, 5]);
});

it("keeps a surviving Markdown draft valid without an editor, but never revives a replaced edit", () => {
  let draft = "original";
  const original = Symbol("original edit");
  let editing = original;
  let editor: HTMLTextAreaElement | null = document.createElement("textarea");
  const target = createSpeechTarget({
    read: () => (editing === original ? draft : null),
    write: (value) => {
      draft = value;
    },
    editor: () => editor,
  });
  editor = null;
  expect(target.append("after blur")).toBe(true);
  expect(draft).toBe("original\n\nafter blur");
  editing = Symbol("replacement edit");
  draft = "replacement comment";
  editor = document.createElement("textarea");
  expect(target.append("old edit transcript")).toBe(false);
  expect(draft).toBe("replacement comment");
});
