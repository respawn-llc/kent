import type { ApprovalDecision, PendingPrompt } from "@/api";

export type PickerSelection =
  | Readonly<{ kind: "none" }>
  | Readonly<{ kind: "suggested"; number: number }>
  | Readonly<{ kind: "neither" }>
  | Readonly<{ kind: "freeform" }>
  | Readonly<{ kind: "approval"; decision: ApprovalDecision }>;

export type PickerDraft = Readonly<{
  selection: PickerSelection;
  commentary: string;
  status: "tentative" | "pointer-selected" | "answered" | "declined";
}>;

export type PickerState = Readonly<{
  current: string | null;
  drafts: ReadonlyMap<string, PickerDraft>;
}>;

export type PickerAction =
  | Readonly<{ kind: "sync" | "confirm" | "decline" }>
  | Readonly<{ kind: "select" | "activate"; selection: PickerSelection }>
  | Readonly<{ kind: "commentary"; text: string }>
  | Readonly<{ kind: "navigate"; direction: -1 | 1 }>;
export type PickerTransition = Readonly<{
  state: PickerState;
  effect: "none" | "focus-field" | "submit";
}>;

export function emptyPickerState(): PickerState {
  return { current: null, drafts: new Map() };
}

export function pickerBatch(prompts: readonly PendingPrompt[]): readonly PendingPrompt[] {
  const first = prompts[0];
  return first === undefined ? [] : prompts.filter((prompt) => prompt.stepID === first.stepID);
}

export function transitionPicker(
  previous: PickerState,
  prompts: readonly PendingPrompt[],
  action: PickerAction,
): PickerTransition {
  const batch = pickerBatch(prompts);
  const drafts = new Map(
    batch.map((prompt) => [
      prompt.toolCallID,
      previous.drafts.get(prompt.toolCallID) ?? initialDraft(prompt),
    ]),
  );
  const current = currentAfterSync(previous, drafts);
  const state = { current, drafts };
  if (current === null || action.kind === "sync") return { state, effect: "none" };
  const index = batch.findIndex((prompt) => prompt.toolCallID === current);
  if (action.kind === "navigate") {
    return {
      state: {
        ...state,
        current: batch[(index + action.direction + batch.length) % batch.length]?.toolCallID ?? null,
      },
      effect: "none",
    };
  }
  const draft = drafts.get(current);
  if (draft === undefined) throw new Error("Visible prompt has no draft.");
  return editDraft({ current, drafts }, batch, action, draft);
}

function editDraft(
  state: Readonly<{ current: string; drafts: Map<string, PickerDraft> }>,
  batch: readonly PendingPrompt[],
  action: Exclude<PickerAction, { kind: "navigate" }>,
  draft: PickerDraft,
): PickerTransition {
  const { current, drafts } = state;
  const index = batch.findIndex((prompt) => prompt.toolCallID === current);
  if (action.kind === "confirm" && [...drafts.values()].every(isPickerDraftComplete)) {
    return { state, effect: "submit" };
  }
  if (draft.status === "declined") return { state, effect: "none" };
  if (action.kind === "decline") {
    drafts.set(current, { ...draft, status: "declined" });
    return advance(state, batch, index);
  }
  if (action.kind === "commentary") {
    drafts.set(current, { ...draft, commentary: action.text, status: "tentative" });
    return { state, effect: "none" };
  }
  if (action.kind === "select" || action.kind === "activate") {
    const chosen = selectDraft(draft, action);
    drafts.set(current, chosen.draft);
    if (chosen.effect !== null) return { state, effect: chosen.effect };
  }
  const selected = drafts.get(current);
  if (selected === undefined) throw new Error("Visible prompt has no draft.");
  const invalid = invalidAnswer(selected);
  if (invalid !== null) return { state, effect: invalid };
  drafts.set(current, { ...selected, status: "answered" });
  return advance(state, batch, index);
}

function invalidAnswer(selected: PickerDraft): "none" | "focus-field" | null {
  if (selected.selection.kind === "none") return "none";
  if (
    (selected.selection.kind === "neither" || selected.selection.kind === "freeform") &&
    selected.commentary.trim().length === 0
  )
    return "focus-field";
  return null;
}

function currentAfterSync(previous: PickerState, drafts: ReadonlyMap<string, PickerDraft>): string | null {
  if (previous.current !== null) {
    const ids = [...previous.drafts.keys()];
    const index = ids.indexOf(previous.current);
    for (let offset = 0; offset < ids.length; offset++) {
      const candidate = ids[(index + offset) % ids.length];
      if (candidate !== undefined && drafts.has(candidate)) return candidate;
    }
  }
  return drafts.keys().next().value ?? null;
}

function advance(state: PickerState, batch: readonly PendingPrompt[], index: number): PickerTransition {
  for (let offset = 1; offset <= batch.length; offset++) {
    const next = batch[(index + offset) % batch.length];
    const nextDraft = next === undefined ? undefined : state.drafts.get(next.toolCallID);
    if (next !== undefined && nextDraft !== undefined && !isPickerDraftComplete(nextDraft)) {
      return { state: { ...state, current: next.toolCallID }, effect: "none" };
    }
  }
  return { state, effect: "submit" };
}

function selectDraft(
  draft: PickerDraft,
  action: Extract<PickerAction, { kind: "select" | "activate" }>,
): Readonly<{ draft: PickerDraft; effect: "none" | "focus-field" | null }> {
  const selected: PickerDraft = { ...draft, selection: action.selection, status: "tentative" };
  if (action.kind === "select") return { draft: selected, effect: "none" };
  if (action.selection.kind === "neither" || action.selection.kind === "freeform") {
    return { draft: selected, effect: "focus-field" };
  }
  if (
    draft.commentary.length > 0 &&
    (draft.status !== "pointer-selected" || !sameSelection(draft.selection, action.selection))
  ) {
    return { draft: { ...selected, status: "pointer-selected" }, effect: "none" };
  }
  return { draft: selected, effect: null };
}

export function isPickerDraftComplete(draft: PickerDraft): boolean {
  return draft.status === "answered" || draft.status === "declined";
}

export function canConfirmPicker(state: PickerState): boolean {
  const current = state.current === null ? undefined : state.drafts.get(state.current);
  if (current === undefined) return false;
  if ([...state.drafts.values()].every(isPickerDraftComplete)) return true;
  return current.status !== "declined" && invalidAnswer(current) === null;
}

export function sameSelection(left: PickerSelection, right: PickerSelection): boolean {
  if (left.kind !== right.kind) return false;
  if (left.kind === "suggested" && right.kind === "suggested") return left.number === right.number;
  if (left.kind === "approval" && right.kind === "approval") return left.decision === right.decision;
  return true;
}

function initialDraft(prompt: PendingPrompt): PickerDraft {
  const selection: PickerSelection =
    prompt.kind === "ordinary"
      ? prompt.suggestions.length === 0
        ? { kind: "freeform" }
        : prompt.recommendedOptionIndex === null
          ? { kind: "none" }
          : { kind: "suggested", number: prompt.recommendedOptionIndex }
      : { kind: "none" };
  return { selection, commentary: "", status: "tentative" };
}
