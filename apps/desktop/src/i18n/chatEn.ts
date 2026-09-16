import { chatToolRowsEnglish } from "./chatToolRowsEn";
import { chatWorktreeEnglish } from "./chatWorktreeEn";

const chatPickerEnglish = {
  position: "Question {{current}} of {{count}}",
  previous: "Previous question",
  next: "Next question",
  declineShortcut: "Decline to answer (Ctrl+D)",
  decline: "Decline to answer",
  sendingFailed: "Sending failed",
};

export const chatEnglish = {
  tail: {
    jump: "Jump to latest",
    loading: "Loading transcript…",
    loadingLatest: "Loading latest messages…",
    failed: "Could not load transcript",
  },
  picker: chatPickerEnglish,
  worktree: chatWorktreeEnglish,
  toolRows: chatToolRowsEnglish,
};
