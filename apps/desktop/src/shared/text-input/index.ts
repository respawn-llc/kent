export function insertTextAtSelection(input: HTMLTextAreaElement | HTMLInputElement, text: string): void {
  if (
    !input.isConnected ||
    input.disabled ||
    input.readOnly ||
    input.selectionStart === null ||
    input.selectionEnd === null
  )
    return;
  input.setRangeText(text, input.selectionStart, input.selectionEnd, "end");
  input.dispatchEvent(new Event("input", { bubbles: true }));
}
