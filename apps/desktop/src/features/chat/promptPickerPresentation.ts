import type { TFunction } from "i18next";
import type { PendingPrompt } from "@/api";
import { approvalDecisionLabel } from "@/shared/prompt-presentation";
import type { PickerSelection } from "./promptPickerState";

export type PickerOption = Readonly<{
  value: string;
  text: string;
  selection: PickerSelection;
  recommended: boolean;
}>;

export function pickerOptions(prompt: PendingPrompt, t: TFunction): readonly PickerOption[] {
  if (prompt.kind === "approval")
    return prompt.approvalDecisions.map((decision) => ({
      value: decision,
      text: approvalDecisionLabel(decision, t),
      selection: { kind: "approval", decision },
      recommended: false,
    }));
  if (prompt.suggestions.length === 0) return [];
  return [
    ...prompt.suggestions.map((text, index): PickerOption => ({
      value: String(index + 1),
      text,
      selection: { kind: "suggested", number: index + 1 },
      recommended: prompt.recommendedOptionIndex === index + 1,
    })),
    { value: "neither", text: t("task.neitherOption"), selection: { kind: "neither" }, recommended: false },
  ];
}

export function sameSelection(left: PickerSelection, right: PickerSelection): boolean {
  if (left.kind !== right.kind) return false;
  if (left.kind === "suggested" && right.kind === "suggested") return left.number === right.number;
  if (left.kind === "approval" && right.kind === "approval") return left.decision === right.decision;
  return true;
}
