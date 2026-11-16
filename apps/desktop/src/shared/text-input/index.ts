export function insertTextAtSelection(input: HTMLTextAreaElement | HTMLInputElement, text: string): void {
  if (!isEditableTextInput(input)) return;
  input.setRangeText(text, input.selectionStart, input.selectionEnd, "end");
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

export function isEditableTextInput(
  input: Element | null,
): input is (HTMLTextAreaElement | HTMLInputElement) & { selectionStart: number; selectionEnd: number } {
  return (
    (input instanceof HTMLTextAreaElement || input instanceof HTMLInputElement) &&
    input.isConnected &&
    !input.matches(":disabled") &&
    !input.readOnly &&
    input.selectionStart !== null &&
    input.selectionEnd !== null
  );
}
