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
  status: "tentative" | "answered" | "declined";
}>;

export type PickerState = Readonly<{
  current: string | null;
  drafts: ReadonlyMap<string, PickerDraft>;
  batchToolCallIDs: readonly string[];
}>;

type MutablePickerState = Readonly<{
  current: string;
  drafts: Map<string, PickerDraft>;
  batchToolCallIDs: readonly string[];
}>;

type SynchronizedPickerState = Readonly<{
  current: string | null;
  drafts: Map<string, PickerDraft>;
  batchToolCallIDs: readonly string[];
}>;

type DraftEditContext = Readonly<{
  state: MutablePickerState;
  batch: readonly PendingPrompt[];
  index: number;
}>;

export type PickerAction =
  | Readonly<{ kind: "sync" }>
  | Readonly<{ kind: "confirm" }>
  | Readonly<{ kind: "decline" }>
  | Readonly<{
      kind: "select";
      selection: PickerSelection;
      originToolCallID?: string;
    }>
  | Readonly<{
      kind: "activate";
      selection: PickerSelection;
      originToolCallID?: string;
    }>
  | Readonly<{ kind: "pointer-confirm"; selection: PickerSelection; originToolCallID: string }>
  | Readonly<{ kind: "commentary"; text: string; toolCallID?: string }>
  | Readonly<{ kind: "navigate"; direction: -1 | 1 }>;
export type PickerTransition = Readonly<{
  state: PickerState;
  effect: "none" | "focus-field" | "submit";
}>;

type PickerEditAction = Exclude<PickerAction, { kind: "sync" | "navigate" }>;

export function emptyPickerState(): PickerState {
  return { current: null, drafts: new Map(), batchToolCallIDs: [] };
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
  const synchronized = synchronizePicker(previous, prompts);
  if (synchronized === null) return { state: emptyPickerState(), effect: "none" };
  const { batch, state } = synchronized;
  const { current } = state;
  if (current === null || action.kind === "sync") return { state, effect: "none" };
  if (!actionOriginIsCurrent(action, current)) return { state, effect: "none" };
  const currentState: MutablePickerState = {
    current,
    drafts: state.drafts,
    batchToolCallIDs: state.batchToolCallIDs,
  };
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
  const draft = state.drafts.get(current);
  if (draft === undefined) throw new Error("Visible prompt has no draft.");
  return editDraft({ state: currentState, batch, index }, action, draft);
}

function synchronizePicker(
  previous: PickerState,
  prompts: readonly PendingPrompt[],
): Readonly<{ batch: readonly PendingPrompt[]; state: SynchronizedPickerState }> | null {
  const batch = pickerBatch(prompts);
  const first = batch[0];
  if (first === undefined) return null;
  const pendingIDs = new Set(batch.map((prompt) => prompt.toolCallID));
  const sameBatch = previous.batchToolCallIDs.some((id) => pendingIDs.has(id));
  const batchToolCallIDs = [
    ...new Set([
      ...(first.batch?.toolCallIDs ?? []),
      ...(sameBatch ? previous.batchToolCallIDs : []),
      ...pendingIDs,
    ]),
  ];
  const multiplePrompts = batchToolCallIDs.length > 1;
  const drafts = new Map(
    batch.map((prompt) => [
      prompt.toolCallID,
      previous.drafts.get(prompt.toolCallID) ?? initialDraft(prompt, multiplePrompts),
    ]),
  );
  const current = currentAfterSync(previous, drafts);
  return { batch, state: { current, drafts, batchToolCallIDs } };
}

function actionOriginIsCurrent(action: PickerAction, current: string): boolean {
  return (
    (action.kind !== "select" && action.kind !== "activate" && action.kind !== "pointer-confirm") ||
    action.originToolCallID === undefined ||
    action.originToolCallID === current
  );
}

function editDraft(
  context: DraftEditContext,
  action: PickerEditAction,
  draft: PickerDraft,
): PickerTransition {
  const { state, batch, index } = context;
  const { current, drafts } = state;
  if (action.kind === "confirm") {
    if ([...drafts.values()].every(isPickerDraftComplete) && batchReady(batch))
      return { state, effect: "submit" };
    if (draft.status === "declined") return { state, effect: "none" };
    const invalid = invalidAnswer(draft);
    if (invalid !== null) return { state, effect: invalid };
    drafts.set(current, { ...draft, status: "answered" });
    return advance(state, batch, index);
  }
  if (action.kind === "commentary") {
    setCommentary(drafts, current, action);
    return { state, effect: "none" };
  }
  if (draft.status === "declined") return { state, effect: "none" };
  if (action.kind === "decline") {
    drafts.set(current, { ...draft, status: "declined" });
    return advance(state, batch, index);
  }
  if (action.kind === "select") {
    drafts.set(current, selectDraft(draft, action.selection));
    return { state, effect: "none" };
  }
  return activateSelection(context, action, draft);
}

function activateSelection(
  context: DraftEditContext,
  action: Extract<PickerAction, { kind: "activate" | "pointer-confirm" }>,
  draft: PickerDraft,
): PickerTransition {
  const { state, batch, index } = context;
  const selection = action.selection;
  if (action.kind === "pointer-confirm") {
    if (
      selection.kind === "neither" ||
      selection.kind === "freeform" ||
      (state.batchToolCallIDs.length > 1 && draft.commentary.length === 0)
    )
      return { state, effect: "none" };
  }
  const selected = selectDraft(draft, selection);
  state.drafts.set(state.current, selected);
  if (action.kind === "activate") {
    if (selection.kind === "neither" || selection.kind === "freeform")
      return { state, effect: "focus-field" };
    if (state.batchToolCallIDs.length === 1 || draft.commentary.length > 0) return { state, effect: "none" };
  }
  const invalid = invalidAnswer(selected);
  if (invalid !== null) return { state, effect: invalid };
  state.drafts.set(state.current, { ...selected, status: "answered" });
  return advance(state, batch, index);
}

function setCommentary(
  drafts: Map<string, PickerDraft>,
  current: string,
  action: Extract<PickerAction, { kind: "commentary" }>,
): void {
  const target = action.toolCallID ?? current;
  const draft = drafts.get(target);
  if (draft !== undefined && draft.status !== "declined") {
    drafts.set(target, { ...draft, commentary: action.text, status: "tentative" });
  }
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
  return { state, effect: batchReady(batch) ? "submit" : "none" };
}

function batchReady(batch: readonly PendingPrompt[]): boolean {
  return batch.every((prompt) => prompt.batch === undefined || prompt.batch.unmaterializedCount === 0);
}

function selectDraft(draft: PickerDraft, selection: PickerSelection): PickerDraft {
  return { ...draft, selection, status: "tentative" };
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

function initialDraft(prompt: PendingPrompt, multiplePrompts: boolean): PickerDraft {
  const selection: PickerSelection =
    prompt.kind === "ordinary"
      ? prompt.suggestions.length === 0
        ? { kind: "freeform" }
        : prompt.recommendedOptionIndex === null || !multiplePrompts
          ? { kind: "none" }
          : { kind: "suggested", number: prompt.recommendedOptionIndex }
      : { kind: "none" };
  return { selection, commentary: "", status: "tentative" };
}
