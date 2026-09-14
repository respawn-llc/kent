import type { PromptAnswerBatchInput, QuestionAnswerInput, QuestionAttentionItem } from "@/api";
import { promptAnswerEntry } from "@/api";
import type { QuestionSelectionState } from "./TaskDetailQuestionState";

export type QuestionAnswerMutation = Readonly<{
  isPending: boolean;
  mutateAsync(
    input: QuestionAnswerInput,
    attempt: Readonly<{
      attention: QuestionAttentionItem;
      selection: QuestionSelectionState;
    }>,
  ): Promise<unknown>;
}>;

export function questionAnswerBatchInput(input: QuestionAnswerInput): PromptAnswerBatchInput {
  return {
    sessionID: input.sessionID,
    stepID: input.stepID,
    entries: [promptAnswerEntry(input)],
  };
}
