import type { PromptAnswerBatchEntryInput, QuestionAnswerInput } from "./clientInputs";

export function promptAnswerEntry(input: QuestionAnswerInput): PromptAnswerBatchEntryInput {
  return input.kind === "approval"
    ? {
        kind: "approval",
        toolCallID: input.toolCallID,
        decision: input.decision,
        commentary: optionalText(input.commentary),
      }
    : {
        kind: "question",
        toolCallID: input.toolCallID,
        selectedOptionNumber: input.selectedOptionNumber,
        freeform: optionalText(input.freeformAnswer),
      };
}

function optionalText(value: string): string | null {
  return value.trim().length === 0 ? null : value;
}
