import { promptAnswerEntry, type PendingPrompt, type PromptAnswerBatchEntryInput } from "@/api";
import { isPickerDraftComplete, type PickerDraft } from "./promptPickerState";

export function pickerAnswer(
  prompt: PendingPrompt,
  draft: PickerDraft | undefined,
): PromptAnswerBatchEntryInput {
  if (draft === undefined || !isPickerDraftComplete(draft))
    throw new Error("Cannot submit an unfinished prompt.");
  if (draft.status === "declined") return { kind: "declined", toolCallID: prompt.toolCallID };
  const identity = { toolCallID: prompt.toolCallID, sessionID: prompt.sessionID, stepID: prompt.stepID };
  switch (draft.selection.kind) {
    case "approval":
      return promptAnswerEntry({
        ...identity,
        kind: "approval",
        decision: draft.selection.decision,
        commentary: draft.commentary,
      });
    case "suggested":
    case "neither":
    case "freeform":
      return promptAnswerEntry({
        ...identity,
        kind: "ordinary",
        selectedOptionNumber: draft.selection.kind === "suggested" ? draft.selection.number : null,
        freeformAnswer: draft.commentary,
      });
    case "none":
      throw new Error("Cannot submit a prompt without an answer.");
  }
}
