import type { PromptAnswerBatchInput, QuestionAnswerInput, QuestionAttentionItem } from "@/api";
import { promptAnswerEntry } from "@/api";
import type { QuestionSelectionState } from "./TaskDetailQuestionState";

export type QuestionAnswerAction = Readonly<{
  submit(
    input: QuestionAnswerInput,
    attempt: Readonly<{
      attention: QuestionAttentionItem;
      selection: QuestionSelectionState;
    }>,
  ): void;
}>;

export function questionAnswerBatchInput(input: QuestionAnswerInput): PromptAnswerBatchInput {
  return {
    sessionID: input.sessionID,
    stepID: input.stepID,
    entries: [promptAnswerEntry(input)],
  };
}
