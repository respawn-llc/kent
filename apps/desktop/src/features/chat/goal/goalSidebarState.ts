import type { ChatGoalFact, ChatGoalStatus } from "@/api";

export type GoalMutationIntent =
  | Readonly<{ kind: "goal"; preview: Readonly<{ objective: string; status: ChatGoalStatus }> }>
  | Readonly<{ kind: "clear" }>;

export type GoalObservationState =
  | Readonly<{ kind: "loading" | "observed"; fact: ChatGoalFact | null }>
  | Readonly<{ kind: "error"; error: Error; fact: ChatGoalFact | null }>;

export type DraftState = Readonly<{ base: string; draft: string }>;
export type GoalSidebarDerivedState = Readonly<{
  fact: ChatGoalFact | null;
  pendingIntent: GoalMutationIntent | null;
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
  observation: GoalObservationState,
  pendingIntent: GoalMutationIntent | null,
  localDraft: DraftState,
): GoalSidebarDerivedState {
  const fact = observation.fact;
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

export function reconcileDraftState(
  localDraft: DraftState,
  fact: ChatGoalFact | null,
  pendingIntent: GoalMutationIntent | null,
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

export function localDraftForFact(fact: ChatGoalFact | null): DraftState {
  const objective = fact?.goal?.objective ?? "";
  return { base: objective, draft: objective };
}

function goalPresentation(
  fact: ChatGoalFact | null,
  pendingIntent: GoalMutationIntent | null,
  draft: string,
): Readonly<{ objective: string; status: ChatGoalStatus | null; createdAt: string | null }> {
  if (pendingIntent?.kind === "clear") return { objective: "", status: null, createdAt: null };
  if (pendingIntent?.kind === "goal") return pendingGoalPresentation(fact, pendingIntent, draft);
  return { objective: draft, status: fact?.goal?.status ?? null, createdAt: fact?.goal?.createdAt ?? null };
}

function pendingGoalPresentation(
  fact: ChatGoalFact | null,
  pendingIntent: Extract<GoalMutationIntent, { kind: "goal" }>,
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
