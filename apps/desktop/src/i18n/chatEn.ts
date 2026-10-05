import { chatToolRowsEnglish } from "./chatToolRowsEn";
import { chatWorktreeEnglish } from "./chatWorktreeEn";

const chatPickerEnglish = {
  position: "Question {{current}} of {{count}}",
  previous: "Previous question",
  next: "Next question",
  declineShortcut: "Decline to answer (Ctrl+D)",
  decline: "Decline to answer",
  sendingFailed: "Sending failed",
  sendingFailedBody: "Your answers are still here. Review them and submit again.",
};

export const chatEnglish = {
  openingFailed: "Chat could not be loaded. Retry the failed read or go back.",
  tail: {
    jump: "Jump to latest",
    loading: "Loading transcript…",
    loadingLatest: "Loading latest messages…",
    failed: "Could not load transcript",
  },
  newChat: "New Chat",
  savingDraft: "Saving draft before leaving…",
  workspace: "Choose workspace",
  workspacePending: "Wait for the first Chat action to finish before changing workspace.",
  defaultWorkspaceMissing: "The Project default workspace is unavailable.",
  worktreeSessionRequired: "Create a Session before using Worktree commands.",
  picker: chatPickerEnglish,
  worktree: chatWorktreeEnglish,
  toolRows: chatToolRowsEnglish,
};
