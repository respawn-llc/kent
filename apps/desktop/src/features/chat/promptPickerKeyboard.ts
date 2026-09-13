import type { KeyboardEvent, RefObject } from "react";
import type { PickerAction } from "./promptPickerState";
import type { PickerOption } from "./promptPickerPresentation";

type KeyboardControls = Readonly<{
  answerArea: RefObject<HTMLDivElement | null>;
  field: RefObject<HTMLTextAreaElement | null>;
  options: readonly PickerOption[];
  selected: PickerOption | undefined;
  disabled: boolean;
  act(action: PickerAction): void;
}>;

export function promptPickerKeyboard(event: KeyboardEvent<HTMLDivElement>, controls: KeyboardControls): void {
  if (event.nativeEvent.isComposing) return;
  const inField = event.target === controls.field.current;
  if (event.key === "Tab") {
    consume(event);
    if (inField) controls.answerArea.current?.focus();
    else controls.field.current?.focus();
    return;
  }
  if (event.key === "Enter" && !event.shiftKey) {
    consume(event);
    controls.act({ kind: "confirm" });
    return;
  }
  if (!inField) answerAreaKey(event, controls);
}

function answerAreaKey(event: KeyboardEvent<HTMLDivElement>, controls: KeyboardControls): void {
  switch (event.key) {
    case "ArrowLeft":
    case "ArrowRight":
      consume(event);
      controls.act({ kind: "navigate", direction: event.key === "ArrowLeft" ? -1 : 1 });
      return;
    case "ArrowUp":
    case "ArrowDown":
      consume(event);
      if (!controls.disabled) selectAdjacent(controls, event.key === "ArrowDown" ? 1 : -1);
      return;
    case "d":
    case "D":
      if (event.ctrlKey) {
        consume(event);
        controls.act({ kind: "decline" });
      }
  }
}

function selectAdjacent(controls: KeyboardControls, direction: -1 | 1): void {
  const { options, selected } = controls;
  if (options.length === 0) return;
  const next =
    selected === undefined
      ? direction === 1
        ? 0
        : options.length - 1
      : (options.indexOf(selected) + direction + options.length) % options.length;
  const option = options[next];
  if (option !== undefined) controls.act({ kind: "select", selection: option.selection });
}

function consume(event: KeyboardEvent<HTMLDivElement>): void {
  event.preventDefault();
  event.stopPropagation();
}
