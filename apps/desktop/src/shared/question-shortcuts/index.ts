import { useEffect, type RefObject } from "react";

import { isEditableTextInput } from "@/shared/text-input";

export function useQuestionNumberShortcuts({
  questionRef,
  editorRef,
  optionCount,
  neitherIndex,
  enabled,
  onSelect,
}: Readonly<{
  questionRef: RefObject<HTMLElement | null>;
  editorRef: RefObject<HTMLTextAreaElement | null>;
  optionCount: number;
  neitherIndex: number | undefined;
  enabled: boolean;
  onSelect(index: number): void;
}>): void {
  useEffect(() => {
    if (!enabled) return;
    const select = (event: KeyboardEvent) => {
      if (!isChoiceKey(event) || editingText(document.activeElement)) return;
      const first = Array.from(document.querySelectorAll<HTMLElement>("[data-question-number-shortcuts]"))
        .filter(topIsVisible)
        .sort((left, right) => left.getBoundingClientRect().top - right.getBoundingClientRect().top)[0];
      if (first !== questionRef.current) return;
      const index = event.key === "0" ? neitherIndex : Number(event.key) - 1;
      if (index === undefined || index >= optionCount) return;
      event.preventDefault();
      event.stopPropagation();
      onSelect(index);
      editorRef.current?.focus();
    };
    document.addEventListener("keydown", select);
    return () => {
      document.removeEventListener("keydown", select);
    };
  }, [editorRef, enabled, neitherIndex, onSelect, optionCount, questionRef]);
}

function topIsVisible(element: HTMLElement): boolean {
  if (element.closest("[hidden], [inert]") !== null) return false;
  const rect = element.getBoundingClientRect();
  if (!topFitsViewport(rect)) return false;
  for (let ancestor: HTMLElement | null = element; ancestor !== null; ancestor = ancestor.parentElement) {
    const style = getComputedStyle(ancestor);
    if (style.display === "none" || ["hidden", "collapse"].includes(style.visibility)) return false;
    if (ancestor !== element && ["auto", "scroll", "hidden", "clip"].includes(style.overflowY)) {
      const clip = ancestor.getBoundingClientRect();
      if (rect.top < clip.top || rect.top >= clip.bottom) return false;
    }
  }
  return true;
}

function topFitsViewport(rect: DOMRect): boolean {
  return (
    rect.width > 0 &&
    rect.height > 0 &&
    rect.top >= 0 &&
    rect.top < window.innerHeight &&
    rect.right > 0 &&
    rect.left < window.innerWidth
  );
}

function editingText(focused: Element | null): boolean {
  return isEditableTextInput(focused) || (focused instanceof HTMLElement && focused.isContentEditable);
}

function isChoiceKey(event: KeyboardEvent): boolean {
  return (
    !event.defaultPrevented &&
    !event.repeat &&
    !event.isComposing &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.altKey &&
    !event.shiftKey &&
    event.key.length === 1 &&
    event.key >= "0" &&
    event.key <= "9"
  );
}
