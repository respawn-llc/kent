import type { TFunction } from "i18next";
import type { KeyboardEvent } from "react";

import { decodeWorkflowLabelError, errorMessage, type ProjectLabel } from "@/api";
import { isTextFieldSubmitShortcut, type TextFieldSubmitShortcutPolicy } from "@/app-facade";
import type { LabelChooserInvocation } from "./LabelChooser";
import type { LabelFilterCondition, LabelResultRowSelection } from "./LabelChooserRows";
import type { LabelFilterState } from "./labelFilterState";

export function labelMutationErrorMessage(error: unknown, t: TFunction): string {
  const labelError = decodeWorkflowLabelError(error);
  if (labelError === null) {
    return errorMessage(error);
  }
  switch (labelError.reason) {
    case "invalid_name":
      return t("labels.invalidName");
    case "name_conflict":
      return t("labels.nameConflict");
    case "catalog_limit":
      return t("labels.catalogLimit");
    case "project_not_found":
      return t("labels.projectMissing");
    case "label_not_found":
      return t("labels.labelMissing");
    case "task_not_found":
    case "wrong_project":
    case "invalid_filter":
    case "invalid_mutation":
      return t("labels.mutationFailed");
  }
}

export function handleLabelChooserSearchKeyDown({
  canCreate,
  catalogMutationPending,
  catalogAtLimit,
  choices,
  createLabel,
  event,
  highlightedIndex,
  invocation,
  policy,
  setHighlightedIndex,
}: Readonly<{
  canCreate: boolean;
  catalogMutationPending: boolean;
  catalogAtLimit: boolean;
  choices: readonly (Readonly<{ kind: "unlabeled" }> | Readonly<{ kind: "label"; label: ProjectLabel }>)[];
  createLabel(): void;
  event: KeyboardEvent<HTMLInputElement>;
  highlightedIndex: number | null;
  invocation: LabelChooserInvocation;
  policy: TextFieldSubmitShortcutPolicy;
  setHighlightedIndex(update: (current: number | null) => number): void;
}>): void {
  if (handleLabelChoiceNavigation(event, choices.length, setHighlightedIndex)) {
    return;
  }
  if (event.key !== "Enter") {
    return;
  }
  if (choices.length > 0) {
    event.preventDefault();
    activateLabelChoice(choices, Math.min(highlightedIndex ?? 0, choices.length - 1), invocation);
    return;
  }
  if (canCreate && !catalogAtLimit && !catalogMutationPending) {
    event.preventDefault();
    createLabel();
    return;
  }
  if (isTextFieldSubmitShortcut(event, policy)) {
    event.preventDefault();
    event.stopPropagation();
  }
}

function activateLabelChoice(
  choices: readonly (Readonly<{ kind: "unlabeled" }> | Readonly<{ kind: "label"; label: ProjectLabel }>)[],
  index: number,
  invocation: LabelChooserInvocation,
): void {
  const choice = choices[index];
  if (choice === undefined) {
    return;
  }
  if (choice.kind === "unlabeled") {
    selectUnlabeled(invocation);
    return;
  }
  const selection = labelResultRowSelection(invocation, choice.label.id);
  selectLabel(invocation, choice.label.id, selection.kind === "binary" ? !selection.selected : true);
}

export function labelResultRowSelection(
  invocation: LabelChooserInvocation,
  labelID: string,
): LabelResultRowSelection {
  if (invocation.kind === "assignment") {
    return {
      kind: "binary",
      selected: invocation.selectedLabelIDs.includes(labelID),
    };
  }
  return {
    kind: "condition",
    state: labelFilterCondition(invocation.state, labelID),
  };
}

export function selectLabel(invocation: LabelChooserInvocation, labelID: string, selected: boolean): void {
  if (invocation.kind === "filter") {
    invocation.onAction({ type: "named.cycle", labelID });
    return;
  }
  invocation.onSelectionChange(labelID, selected);
}

export function selectUnlabeled(invocation: LabelChooserInvocation): void {
  if (invocation.kind === "filter") {
    invocation.onAction({ type: "unlabeled.toggle" });
  }
}

export function removeDeletedSelection(invocation: LabelChooserInvocation, labelID: string): void {
  if (invocation.kind === "filter") {
    invocation.onAction({ type: "label.deleted", labelID });
    return;
  }
  if (invocation.selectedLabelIDs.includes(labelID)) {
    invocation.onSelectionChange(labelID, false);
  }
}

function labelFilterCondition(state: LabelFilterState, labelID: string): LabelFilterCondition {
  if (state.filter.kind !== "named") {
    return "neutral";
  }
  if (state.filter.labelIDs.includes(labelID)) {
    return "included";
  }
  return state.filter.excludedLabelIDs.includes(labelID) ? "excluded" : "neutral";
}

function handleLabelChoiceNavigation(
  event: KeyboardEvent<HTMLInputElement>,
  choiceCount: number,
  setHighlightedIndex: (update: (current: number | null) => number) => void,
): boolean {
  if (choiceCount === 0) {
    return false;
  }
  if (event.key === "ArrowDown") {
    event.preventDefault();
    setHighlightedIndex((current) => (current === null ? 0 : (current + 1) % choiceCount));
    return true;
  }
  if (event.key === "ArrowUp") {
    event.preventDefault();
    setHighlightedIndex((current) =>
      current === null ? choiceCount - 1 : (current - 1 + choiceCount) % choiceCount,
    );
    return true;
  }
  return false;
}
