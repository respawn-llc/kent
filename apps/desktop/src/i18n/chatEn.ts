import { chatToolRowsEnglish } from "./chatToolRowsEn";

const chatPickerEnglish = {
  position: "Question {{current}} of {{count}}",
  previous: "Previous question",
  next: "Next question",
  declineShortcut: "Decline to answer (Ctrl+D)",
  decline: "Decline to answer",
  sendingFailed: "Sending failed",
};

export const chatEnglish = {
  picker: chatPickerEnglish,
  worktree: {
    title: "Worktree",
    refresh: "Refresh worktrees",
    create: "Create worktree",
    switch: "Switch",
    delete: "Delete worktree",
    empty: "No worktrees",
    external: "External",
    missing: "Missing",
  },
  toolRows: chatToolRowsEnglish,
};
