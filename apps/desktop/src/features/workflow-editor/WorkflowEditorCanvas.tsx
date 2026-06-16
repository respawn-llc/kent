import { useTranslation } from "react-i18next";

import type { WorkflowInspectorSelection } from "../../app/sidebarContext";
import { useAppServices } from "../../app/useAppServices";
import { useStatusController } from "../../app/useStatusController";
import { WorkflowGraphCanvas } from "./WorkflowGraphCanvas";
import type { WorkflowGraphLayout } from "./workflowGraphLayout";
import {
  workflowDeleteNeedsConfirmation,
  workflowDeletionConfirmationCounts,
} from "./workflowDeleteConfirmationPolicy";
import {
  workflowEditorGraphMutationWarnings,
} from "./workflowEditorGraphMutations";
import {
  cascadeRowCount,
  copyWorkflowNodeText,
  deleteWarningTranslationKey,
  dispatchGraphDeletion,
  graphEditWarningTranslationKey,
  nextGraphDeleteRequestID,
  planGraphDeletion,
  planGraphExtraction,
  type PendingGraphMutation,
} from "./workflowEditorGraphMutationPlanning";
import type { WorkflowEditorDraftAction, WorkflowEditorDraftState } from "./workflowEditorDraft";
import { newWorkflowTopologyID } from "./workflowTopologyID";
import type { WorkflowGraphSelection } from "./workflowGraphSelection";

export type WorkflowEditorCanvasProps = Readonly<{
  graph: WorkflowGraphLayout;
  surface: "route" | "sidebar";
  draftState: WorkflowEditorDraftState | null;
  dispatch: (action: WorkflowEditorDraftAction) => void;
  deleteRequestIndexRef: { current: number };
  inspect: (selection: WorkflowInspectorSelection) => void;
  closeDeletedNodeInspector: (selection: WorkflowGraphSelection) => void;
  onPendingGraphMutationChange: (mutation: PendingGraphMutation | null) => void;
  openDeleteConfirmation: (mutation: PendingGraphMutation) => Promise<void>;
  workflowID: string;
}>;

export function WorkflowEditorCanvas({
  graph,
  surface,
  draftState,
  dispatch,
  deleteRequestIndexRef,
  inspect,
  closeDeletedNodeInspector,
  onPendingGraphMutationChange,
  openDeleteConfirmation,
  workflowID,
}: WorkflowEditorCanvasProps) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const { push: pushStatus } = useStatusController();

  const handleDeleteSelection = (selection: WorkflowGraphSelection): void => {
    if (draftState === null) {
      return;
    }
    const plannedDelete = planGraphDeletion(draftState.draft, selection);
    onPendingGraphMutationChange(null);
    if (plannedDelete.kind === "blocked") {
      pushStatus({
        body: t(deleteWarningTranslationKey(plannedDelete.warning)),
        id:
          plannedDelete.warning === workflowEditorGraphMutationWarnings.startNodeDelete
            ? "workflow-initial-node-delete-blocked"
            : "workflow-delete-blocked",
        title: t("workflowEditor.deleteBlockedTitle"),
        tone:
          plannedDelete.warning === workflowEditorGraphMutationWarnings.startNodeDelete
            ? "warning"
            : "danger",
      });
      return;
    }
    const counts = workflowDeletionConfirmationCounts(draftState.draft, plannedDelete.summary);
    if (workflowDeleteNeedsConfirmation(counts)) {
      const deleteRequest = {
        action: { kind: "delete", selection },
        counts,
        requestID: nextGraphDeleteRequestID(workflowID, deleteRequestIndexRef),
        summary: plannedDelete.summary,
      } satisfies PendingGraphMutation;
      onPendingGraphMutationChange(deleteRequest);
      void openDeleteConfirmation(deleteRequest);
      return;
    }
    dispatchGraphDeletion(selection, dispatch);
    closeDeletedNodeInspector(selection);
  };

  const handleExtractNodeFromGroup = (nodeID: string): void => {
    if (draftState === null) {
      return;
    }
    onPendingGraphMutationChange(null);
    const input = {
      nodeID,
      rehomedIncomingTransitionGroupID: newWorkflowTopologyID("transitionGroup"),
    };
    const plannedExtraction = planGraphExtraction(draftState.draft, input);
    if (plannedExtraction.kind === "blocked") {
      pushStatus({
        body: t(graphEditWarningTranslationKey(plannedExtraction.warning)),
        id: "workflow-extract-node-from-group-blocked",
        title: t("workflowEditor.graphEditBlockedTitle"),
        tone: "warning",
      });
      return;
    }
    if (cascadeRowCount(plannedExtraction.summary) > 0) {
      const counts = workflowDeletionConfirmationCounts(draftState.draft, plannedExtraction.summary);
      const extractionRequest = {
        action: { graphVersion: draftState.graphVersion, input, kind: "extract" },
        counts,
        requestID: nextGraphDeleteRequestID(workflowID, deleteRequestIndexRef),
        summary: plannedExtraction.summary,
      } satisfies PendingGraphMutation;
      onPendingGraphMutationChange(extractionRequest);
      void openDeleteConfirmation(extractionRequest);
      return;
    }
    dispatch({ input, type: "extractNodeFromGroup" });
  };

  return (
    <WorkflowGraphCanvas
      graph={graph}
      keyboardScope={surface === "route" ? "global" : "focused"}
      toolbarPositionStrategy={surface === "route" ? "fixed" : "absolute"}
      onAddNode={(kind) => {
        dispatch({ input: { id: newWorkflowTopologyID("node"), kind }, type: "addNode" });
      }}
      onAddNodeToGroup={(nodeID, groupID) => {
        dispatch({
          input: {
            groupID,
            inferredTopologyIDs: {
              addedBranchJoinEdgeID: newWorkflowTopologyID("edge"),
              addedBranchJoinTransitionGroupID: newWorkflowTopologyID("transitionGroup"),
              existingBranchJoinEdgeID: newWorkflowTopologyID("edge"),
              existingBranchJoinTransitionGroupID: newWorkflowTopologyID("transitionGroup"),
              fanoutEdgeID: newWorkflowTopologyID("edge"),
            },
            nodeID,
          },
          type: "addNodeToGroup",
        });
      }}
      onConnectNodes={(sourceNodeID, targetNodeID) => {
        dispatch({
          input: {
            edgeID: newWorkflowTopologyID("edge"),
            sourceNodeID,
            targetNodeID,
            transitionGroupID: newWorkflowTopologyID("transitionGroup"),
          },
          type: "connectNodes",
        });
      }}
      onReconnectEdge={(input) => {
        dispatch({ input, type: "reconnectEdge" });
      }}
      onCreateNodeGroup={(nodeID) => {
        dispatch({
          input: {
            groupID: newWorkflowTopologyID("nodeGroup"),
            joinNodeID: newWorkflowTopologyID("node"),
            nodeID,
          },
          type: "createNodeGroupFromNode",
        });
      }}
      onCopyText={async (value) => copyWorkflowNodeText(value, nativeBridge)}
      onDeleteSelection={handleDeleteSelection}
      onEdgeInspect={(edgeID) => {
        inspect({ kind: "edge", edgeID });
      }}
      onGroupInspect={(groupID) => {
        inspect({ kind: "group", groupID });
      }}
      onExtractNodeFromGroup={handleExtractNodeFromGroup}
      onRemoveNodeFromGroup={(nodeID) => {
        dispatch({ nodeID, type: "removeNodeFromGroup" });
      }}
      onNodeInspect={(nodeID) => {
        inspect({ kind: "node", nodeID });
      }}
      onWorkflowInspect={() => {
        inspect({ kind: "workflow" });
      }}
    />
  );
}
