import type { ChatGoalFact, ChatGoalStatus } from "@/api";
import type { ChatGoalDestinationSnapshot, ChatGoalMutationIntent } from "@/app-facade";

export type DraftState = Readonly<{ base: string; draft: string }>;
export type GoalSidebarDerivedState = Readonly<{
  fact: ChatGoalFact | null;
  pendingIntent: ChatGoalMutationIntent | null;
  draftState: DraftState;
  displayedStatus: ChatGoalStatus | null;
  displayedCreatedAt: string | null;
  displayedObjective: string;
  dirty: boolean;
  unavailable: boolean;
  saveAvailable: boolean;
  actionsDisabled: boolean;
}>;

export function deriveGoalSidebarState(
  snapshot: ChatGoalDestinationSnapshot,
  localDraft: DraftState,
): GoalSidebarDerivedState {
  const fact = observedGoalFact(snapshot);
  const pendingIntent = pendingGoalIntent(snapshot);
  const presentation = goalPresentation(fact, pendingIntent, localDraft.draft);
  return {
    actionsDisabled: pendingIntent !== null,
    dirty: localDraft.draft !== localDraft.base,
    displayedCreatedAt: presentation.createdAt,
    displayedObjective: presentation.objective,
    displayedStatus: presentation.status,
    draftState: localDraft,
    fact,
    pendingIntent,
    saveAvailable:
      localDraft.draft !== localDraft.base && nonBlank(localDraft.draft) && pendingIntent === null,
    unavailable: goalUnavailable(fact),
  };
}

export function observedGoalFact(snapshot: ChatGoalDestinationSnapshot): ChatGoalFact | null {
  return snapshot.authority.kind === "observed" ? snapshot.authority.value : null;
}

export function pendingGoalIntent(snapshot: ChatGoalDestinationSnapshot): ChatGoalMutationIntent | null {
  return snapshot.presentation.kind === "unresolved" ? snapshot.presentation.intent : null;
}

export function reconcileDraftState(
  localDraft: DraftState,
  fact: ChatGoalFact | null,
  pendingIntent: ChatGoalMutationIntent | null,
): DraftState {
  if (pendingIntent?.kind === "clear") return { base: "", draft: "" };
  if (pendingIntent?.kind === "goal" && fact?.goal === null) return localDraft;
  if (fact === null) {
    return localDraft.draft === localDraft.base ? { base: "", draft: "" } : { ...localDraft, base: "" };
  }
  const nextBase = fact.goal?.objective ?? "";
  return localDraft.draft === localDraft.base
    ? { base: nextBase, draft: nextBase }
    : { ...localDraft, base: nextBase };
}

function goalPresentation(
  fact: ChatGoalFact | null,
  pendingIntent: ChatGoalMutationIntent | null,
  draft: string,
): Readonly<{ objective: string; status: ChatGoalStatus | null; createdAt: string | null }> {
  if (pendingIntent?.kind === "clear") return { objective: "", status: null, createdAt: null };
  if (pendingIntent?.kind === "goal") return pendingGoalPresentation(fact, pendingIntent, draft);
  return { objective: draft, status: fact?.goal?.status ?? null, createdAt: fact?.goal?.createdAt ?? null };
}

function pendingGoalPresentation(
  fact: ChatGoalFact | null,
  pendingIntent: Extract<ChatGoalMutationIntent, { kind: "goal" }>,
  draft: string,
): Readonly<{ objective: string; status: ChatGoalStatus; createdAt: string | null }> {
  const authoritativeGoal = fact?.goal;
  const objective = pendingIntent.preview.objective === draft ? pendingIntent.preview.objective : draft;
  if (authoritativeGoal === undefined || authoritativeGoal === null) {
    return { objective, status: pendingIntent.preview.status, createdAt: null };
  }
  return {
    objective,
    status: pendingIntent.preview.status,
    createdAt:
      authoritativeGoal.objective === pendingIntent.preview.objective ? authoritativeGoal.createdAt : null,
  };
}

function goalUnavailable(fact: ChatGoalFact | null): boolean {
  return fact?.availability === "agent_capability_missing";
}

export function nonBlank(value: string): boolean {
  return value.trim().length > 0;
}
