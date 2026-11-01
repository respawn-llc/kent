import { flushSync } from "react-dom";

export function appendSpeech(current: string, speech: string): string {
  const separator =
    current.length === 0 || current.endsWith("\n\n") ? "" : current.endsWith("\n") ? "\n" : "\n\n";
  return current + separator + speech;
}

export type SpeechDraft = Readonly<{
  read(): string | null;
  write(text: string): void;
  editor(): HTMLTextAreaElement | null;
}>;

export function createSpeechTarget(draft: SpeechDraft) {
  return {
    append: (text: string): boolean => {
      const current = draft;
      const value = current.read();
      if (value === null) return false;
      const editor = current.editor();
      const selection =
        editor === null
          ? null
          : {
              start: editor.selectionStart,
              end: editor.selectionEnd,
              direction: editor.selectionDirection,
            };
      // Commit controlled editors before restoring selection; otherwise React's
      // later value write moves the caret to the appended text.
      flushSync(() => {
        current.write(appendSpeech(value, text));
      });
      if (editor !== null && selection !== null) {
        editor.setSelectionRange(selection.start, selection.end, selection.direction);
      }
      return true;
    },
  };
}
export type SpeechTarget = ReturnType<typeof createSpeechTarget>;
