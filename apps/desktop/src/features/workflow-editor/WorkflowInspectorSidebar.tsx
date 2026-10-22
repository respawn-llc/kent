import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import type {
  WorkflowInspectorInitialFocus,
  WorkflowInspectorSelection,
  SidebarPageNavigator,
} from "@/app-facade";
import type { WorkflowEditorViewModel } from "./WorkflowEditorViewModel";
import { useWorkflowEditorView } from "./useWorkflowEditorView";
import { WorkflowInspectorHeader } from "./WorkflowInspectorHeader";
import { WorkflowDraftInspectorContent } from "./WorkflowDraftInspector";
import { WorkflowInspectorContent } from "./WorkflowReadonlyInspector";
import { useCachedWorkflowDefinition, useCachedWorkflowValidation } from "./workflowInspectorWiring";

export function WorkflowInspectorSidebar({
  onMissingSelectedNode,
  selection,
  workflowID,
}: Readonly<{
  onMissingSelectedNode?: (() => void) | undefined;
  initialFocus?: WorkflowInspectorInitialFocus | undefined;
  selection: WorkflowInspectorSelection;
  workflowID: string;
}>) {
  const { t } = useTranslation();
  const definition = useCachedWorkflowDefinition(workflowID);
  const validation = useCachedWorkflowValidation(workflowID);
  const selectedNodeMissing =
    selection.kind === "node" &&
    definition !== undefined &&
    !definition.nodes.some((node) => node.id === selection.nodeID);
  useEffect(() => {
    if (selectedNodeMissing) {
      onMissingSelectedNode?.();
    }
  }, [onMissingSelectedNode, selectedNodeMissing]);
  if (selectedNodeMissing && onMissingSelectedNode !== undefined) {
    return null;
  }
  if (definition === undefined) {
    return <p className="text-[var(--color-muted)]">{t("workflowEditor.inspectorUnavailable")}</p>;
  }
  return (
    <WorkflowInspectorContent
      definition={definition}
      selection={selection}
      validation={validation ?? { valid: true, errors: [] }}
    />
  );
}

export function WorkflowEditableInspector({
  model,
  selection,
  initialFocus,
  onMissingSelectedNode,
}: Readonly<{
  model: WorkflowEditorViewModel;
  selection: WorkflowInspectorSelection;
  initialFocus?: WorkflowInspectorInitialFocus | undefined;
  onMissingSelectedNode: () => void;
}>) {
  const controller = useWorkflowEditorView(model);
  const missing =
    controller !== null &&
    selection.kind === "node" &&
    !controller.draft.nodes.some((node) => node.id === selection.nodeID);
  useEffect(() => {
    if (missing) onMissingSelectedNode();
  }, [missing, onMissingSelectedNode]);
  if (controller === null || missing) return null;
  return (
    <WorkflowDraftInspectorContent
      controller={controller}
      selection={selection}
      initialFocus={initialFocus}
    />
  );
}

export function WorkflowEditableInspectorDestination({
  model,
  navigator,
  workflowID,
  selection,
  initialFocus,
}: Readonly<{
  model: WorkflowEditorViewModel;
  navigator: SidebarPageNavigator;
  workflowID: string;
  selection: WorkflowInspectorSelection;
  initialFocus?: WorkflowInspectorInitialFocus | undefined;
}>) {
  return (
    <>
      <WorkflowInspectorHeader selection={selection} workflowID={workflowID} onDeleted={navigator.close} />
      <WorkflowEditableInspector
        model={model}
        selection={selection}
        initialFocus={initialFocus}
        onMissingSelectedNode={navigator.close}
      />
    </>
  );
}
