import { useAtomValue, useAtomSet } from "@effect/atom-react";
import { errorMessage } from "@/api";
import type { WorkflowEditorViewModel } from "./WorkflowEditorViewModel";

export function useWorkflowEditorView(model: WorkflowEditorViewModel) {
  const view = useWorkflowEditorDraftView(model);
  const graph = useAtomValue(model.graph);
  if (view === null) return null;
  return {
    ...view,
    derivedWiring: graph.draftDerivedWiring,
    draftValidation: graph.draftValidation,
    executionValidation: graph.executionValidation,
  } as const;
}

export function useWorkflowEditorDraftView(model: WorkflowEditorViewModel) {
  const { draftState, dirtyState } = useAtomValue(model.state);
  const data = useAtomValue(model.data);
  const saving = useAtomValue(model.saveState);
  const edit = useAtomSet(model.edit);
  const save = useAtomSet(model.save);
  if (draftState === null) return null;
  return {
    dispatch: edit,
    dirty: dirtyState,
    draft: draftState.draft,
    derivedWiring: draftState.draft.derivedWiring,
    draftValidation: null,
    executionValidation: data.validationQuery.data ?? null,
    saving: saving.saving,
    saveError: saving.error === null ? null : errorMessage(saving.error),
    saveBlockers: saving.blockers,
    saveValidation: saving.validation,
    save: () => {
      save(undefined);
    },
    state: draftState,
    workflowID: draftState.source.workflow.id,
  } as const;
}

export type WorkflowEditorView = NonNullable<ReturnType<typeof useWorkflowEditorView>>;
