/* eslint-disable complexity, max-lines -- The route coordinates data loading, draft lifecycle, save, and floating islands. */
import { useCallback, useEffect, useMemo, useReducer, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { CircleQuestionMark } from "lucide-react";

import {
  emptyWorkflowDerivedWiring,
  type WorkflowDefinition,
  type WorkflowGraphSaveConfirmation,
  type WorkflowGraphSaveImpact,
  type WorkflowGraphSavePreview,
  type WorkflowValidation,
} from "../../api";
import { errorMessage } from "../../api/errors";
import { useSidebar } from "../../app/sidebarContext";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import { useNativeDialogFallback } from "../../app/useNativeDialogFallback";
import { useStatusController } from "../../app/useStatusController";
import { useWindowChromeTitle } from "../../app/windowChromeTitle";
import { Button, ErrorState, FloatingNoticeIsland, LoadingState } from "../../ui";
import { cx } from "../../ui/classes";
import { WorkflowValidationIssues } from "../workflow/WorkflowValidationIssues";
import { WorkflowGraphCanvas } from "./WorkflowGraphCanvas";
import { WorkflowDeleteConfirmationFallbackDialog } from "./WorkflowDeleteConfirmationWindow";
import {
  workflowDeleteConfirmationCountsFromSummary,
  workflowDeleteConfirmationWindowOptions,
} from "./workflowDeleteConfirmationModel";
import { useWorkflowGraphDeleteConfirmationListener } from "./useWorkflowGraphDeleteConfirmationListener";
import { layoutWorkflowGraph, type WorkflowGraphLayout } from "./workflowGraphLayout";
import { useWorkflowEditorData, type WorkflowEditorData } from "./useWorkflowEditorData";
import {
  initializeWorkflowEditorDraft,
  type DraftWorkflowDefinition,
  type WorkflowEditorDraftAction,
  workflowDefinitionFromDraft,
  workflowEditorDirtyState,
  workflowEditorDraftGraph,
  workflowEditorDraftMetadata,
  workflowEditorDraftReducer,
  type WorkflowEditorDraftState,
} from "./workflowEditorDraft";
import {
  useRegisterWorkflowEditorDraftController,
  type WorkflowEditorDraftController,
} from "./workflowEditorDraftBridgeCore";
import {
  deleteWorkflowEdge,
  deleteWorkflowNode,
  deleteWorkflowNodeGroup,
  workflowEditorGraphMutationWarnings,
  type WorkflowEditorCascadeSummary,
} from "./workflowEditorGraphMutations";
import { newWorkflowTopologyID } from "./workflowTopologyID";
import type { WorkflowGraphSelection } from "./workflowGraphSelection";

export type WorkflowEditorRouteProps = Readonly<{
  projectID: string;
  workflowID: string;
}>;

export function WorkflowEditorRoute({ projectID, workflowID }: WorkflowEditorRouteProps) {
  const { t } = useTranslation();
  const { api, nativeBridge } = useAppServices();
  const { openSidebar } = useSidebar();
  const { push: pushStatus } = useStatusController();
  const data = useWorkflowEditorData(projectID, workflowID);
  const workflow = data.workflowQuery.data?.workflow;
  const [draftState, dispatch] = useReducer(workflowEditorDraftStateReducer, null);
  const dirty =
    draftState === null
      ? { dirty: false, graphDirty: false, metadataDirty: false }
      : workflowEditorDirtyState(draftState);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [saveBlockers, setSaveBlockers] = useState<readonly string[]>([]);
  const [saveConfirmationPreviewEntry, setSaveConfirmationPreviewEntry] =
    useState<WorkflowSaveConfirmationPreviewEntry | null>(null);
  const saveConfirmationPreviewKey =
    draftState === null ? "" : workflowSaveConfirmationPreviewKey(draftState);
  const saveConfirmationPreview =
    saveConfirmationPreviewEntry?.key === saveConfirmationPreviewKey
      ? saveConfirmationPreviewEntry.preview
      : null;
  function setSaveConfirmationPreview(preview: WorkflowGraphSavePreview | null): void {
    setSaveConfirmationPreviewEntry(
      preview === null ? null : { key: saveConfirmationPreviewKey, preview },
    );
  }
  const [pendingDelete, setPendingDelete] = useState<PendingGraphDelete | null>(null);
  const pendingDeleteRef = useRef<PendingGraphDelete | null>(null);
  const deleteRequestIndexRef = useRef(0);
  const draftDefinition =
    draftState === null ? data.workflowQuery.data : workflowDefinitionFromDraft(draftState.draft);
  const draftValidationQuery = useWorkflowDraftValidationQuery(workflowID, draftState);
  const draftValidation = draftValidationQuery.data?.draft ?? null;
  const draftDerivedWiring =
    draftValidationQuery.data?.derivedWiring ?? draftDefinition?.derivedWiring ?? emptyWorkflowDerivedWiring;
  const executionValidation = draftValidationQuery.data?.execution ?? data.validationQuery.data ?? null;
  const layoutQuery = useWorkflowGraphLayoutQuery(
    workflowID,
    draftDefinition,
    draftState?.graphVersion ?? 0,
    draftValidation ?? executionValidation,
  );
  useWindowChromeTitle(workflow === undefined ? t("workflowEditor.title") : workflow.name);

  useEffect(() => {
    const source = data.workflowQuery.data;
    if (source === undefined) {
      return;
    }
    if (draftState === null) {
      dispatch({ source, type: "reset" });
      return;
    }
    if (source.workflow.version === draftState.source.workflow.version) {
      return;
    }
    if (dirty.dirty) {
      if (
        draftState.conflict?.workflow.version !== source.workflow.version &&
        draftState.acknowledgedConflictVersion !== source.workflow.version
      ) {
        dispatch({ source, type: "conflict" });
      }
      return;
    }
    dispatch({ source, type: "reset" });
  }, [data.workflowQuery.data, dirty.dirty, draftState]);

  const fallbackDraftState = draftState ?? initializeWorkflowEditorDraft(emptyWorkflowDefinition(workflowID));
  const controller = useMemo(
    () => ({
      dispatch,
      dirty,
      draft: fallbackDraftState.draft,
      derivedWiring: draftDerivedWiring,
      draftValidation,
      executionValidation,
      save() {
        if (draftState === null || saving) {
          return;
        }
        void saveWorkflowDraft();
      },
      saveBlockers,
      saveError,
      saving,
      state: fallbackDraftState,
      workflowID,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- saveWorkflowDraft is scoped below and reads the same reactive values listed here.
    [
      dirty,
      draftState,
      draftDerivedWiring,
      draftValidation,
      executionValidation,
      fallbackDraftState,
      saveBlockers,
      saveError,
      saving,
      workflowID,
    ],
  );
  useRegisterWorkflowEditorDraftController(controller);
  useEffect(() => {
    pendingDeleteRef.current = pendingDelete;
  }, [pendingDelete]);
  const confirmPendingGraphDelete = useCallback(
    (deleteRequest: PendingGraphDelete) => {
      pendingDeleteRef.current = null;
      setPendingDelete(null);
      if (draftState === null) {
        return;
      }
      const currentPlan = planGraphDeletion(draftState.draft, deleteRequest.selection);
      if (
        currentPlan.kind !== "ready" ||
        !cascadeSummaryEquals(currentPlan.summary, deleteRequest.summary)
      ) {
        pushStatus({
          body: t("workflowEditor.deleteConfirmationStale"),
          id: "workflow-delete-confirmation-stale",
          title: t("workflowEditor.deleteBlockedTitle"),
          tone: "warning",
        });
        return;
      }
      dispatchGraphDeletion(deleteRequest.selection, dispatch);
    },
    [draftState, pushStatus, t],
  );
  const handleGraphDeleteConfirmationListenerError = useCallback(
    (error: unknown) => {
      pushStatus({
        body: t("workflowEditor.deleteConfirmationListenerFailed", { message: errorMessage(error) }),
        id: "workflow-delete-confirmation-listener-failed",
        title: t("workflowEditor.deleteBlockedTitle"),
        tone: "danger",
      });
    },
    [pushStatus, t],
  );
  useWorkflowGraphDeleteConfirmationListener({
    nativeBridge,
    onConfirmed: confirmPendingGraphDelete,
    onListenerError: handleGraphDeleteConfirmationListenerError,
    pendingDeleteRef,
  });
  const deleteConfirmation = useNativeDialogFallback<PendingGraphDelete>({
    errorNoticeID: "workflow-delete-confirmation-window-error",
    errorTitle: t("workflowEditor.deleteCascadeTitle"),
    nativeAvailable: nativeBridge.capabilities.dialogWindows,
    openNative: async (deleteRequest) => {
      await nativeBridge.dialogs.openWindow(
        workflowDeleteConfirmationWindowOptions({
          counts: workflowDeleteConfirmationCountsFromSummary(deleteRequest.summary),
          requestID: deleteRequest.requestID,
          title: t("workflowEditor.deleteCascadeTitle"),
        }),
      );
    },
    renderFallback: (deleteRequest, close) => (
      <WorkflowDeleteConfirmationFallbackDialog
        counts={workflowDeleteConfirmationCountsFromSummary(deleteRequest.summary)}
        onCancel={() => {
          setPendingDelete(null);
          close();
        }}
        onConfirm={() => {
          confirmPendingGraphDelete(deleteRequest);
          close();
        }}
      />
    ),
  });

  const viewState = workflowEditorViewState(data, layoutQuery);
  if (viewState.kind === "loading") {
    return (
      <LoadingState
        appearanceDelayMs={0}
        chromePadding
        contentWidth="full"
        title={t("workflowEditor.loadingTitle")}
      />
    );
  }
  if (viewState.kind === "linkError") {
    return (
      <ErrorState
        body={errorMessage(viewState.error)}
        chromePadding
        contentWidth="full"
        onRetry={() => void data.linksQuery.refetch()}
        retryLabel={t("app.retry")}
        title={t("workflowEditor.linkLoadFailed")}
      />
    );
  }
  if (viewState.kind === "unlinked") {
    return (
      <ErrorState
        body={t("workflowEditor.unlinkedBody")}
        chromePadding
        contentWidth="full"
        reveal={false}
        title={t("workflowEditor.unlinkedTitle")}
      />
    );
  }
  if (viewState.kind === "loadError") {
    return (
      <ErrorState
        body={errorMessage(viewState.error)}
        chromePadding
        contentWidth="full"
        onRetry={() => {
          void data.boardQuery.refetch();
          void data.workflowQuery.refetch();
          void data.validationQuery.refetch();
          void layoutQuery.refetch();
        }}
        retryLabel={t("app.retry")}
        title={t("workflowEditor.loadFailed")}
      />
    );
  }

  const editorRoute = (
    <section
      className="app-region-no-drag fixed inset-0 z-0 h-screen min-h-0 w-screen overflow-hidden"
      data-testid="workflow-editor-route"
    >
      <WorkflowEditorTopChromeBlur />
      <WorkflowGraphCanvas
        graph={viewState.graph}
        onAddNode={(kind) => {
          dispatch({
            input: {
              id: newWorkflowTopologyID("node"),
              kind,
            },
            type: "addNode",
          });
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
        onDeleteSelection={(selection) => {
          if (draftState === null) {
            return;
          }
          const plannedDelete = planGraphDeletion(draftState.draft, selection);
          setPendingDelete(null);
          if (plannedDelete.kind === "blocked") {
            const message = t(deleteWarningTranslationKey(plannedDelete.warning));
            const toastID =
              plannedDelete.warning === workflowEditorGraphMutationWarnings.startNodeDelete
                ? "workflow-initial-node-delete-blocked"
                : "workflow-delete-blocked";
            if (plannedDelete.warning === workflowEditorGraphMutationWarnings.startNodeDelete) {
              pushStatus({
                body: message,
                id: toastID,
                title: t("workflowEditor.deleteBlockedTitle"),
                tone: "warning",
              });
            } else {
              pushStatus({
                body: message,
                id: toastID,
                title: t("workflowEditor.deleteBlockedTitle"),
                tone: "danger",
              });
            }
            return;
          }
          if (cascadeRowCount(plannedDelete.summary) > 1) {
            const deleteRequest = {
              requestID: nextGraphDeleteRequestID(workflowID, deleteRequestIndexRef),
              selection,
              summary: plannedDelete.summary,
            };
            setPendingDelete(deleteRequest);
            void deleteConfirmation.open(deleteRequest);
            return;
          }
          dispatchGraphDeletion(selection, dispatch);
        }}
        onEdgeInspect={(edgeID) => {
          void openSidebar({
            kind: "workflowInspect",
            mode: "overlay",
            selection: { kind: "edge", edgeID },
            workflowID,
          });
        }}
        onGroupInspect={(groupID) => {
          void openSidebar({
            kind: "workflowInspect",
            mode: "overlay",
            selection: { kind: "group", groupID },
            workflowID,
          });
        }}
        onRemoveNodeFromGroup={(nodeID) => {
          dispatch({ nodeID, type: "removeNodeFromGroup" });
        }}
        onNodeInspect={(nodeID) => {
          void openSidebar({
            kind: "workflowInspect",
            mode: "overlay",
            selection: { kind: "node", nodeID },
            workflowID,
          });
        }}
        onWorkflowInspect={() => {
          void openSidebar({
            kind: "workflowInspect",
            mode: "overlay",
            selection: { kind: "workflow" },
            workflowID,
          });
        }}
      />
      <WorkflowEditorStatusIsland
        confirmationPreview={saveConfirmationPreview}
        controller={controller}
        onCancelConfirmation={() => {
          setSaveConfirmationPreview(null);
        }}
        onConfirmSave={() => {
          if (saveConfirmationPreview === null) {
            return;
          }
          void saveWorkflowDraft(saveConfirmationPreview);
        }}
        onDiscard={() => {
          dispatch({ source: controller.state.source, type: "reset" });
        }}
      />
      {deleteConfirmation.fallback}
      <WorkflowEditorLegendIsland />
    </section>
  );

  return createPortal(editorRoute, document.body);

  async function saveWorkflowDraft(confirmedPreview?: WorkflowGraphSavePreview): Promise<void> {
    if (draftState === null) {
      return;
    }
    const latestDirty = workflowEditorDirtyState(draftState);
    if (latestDirty.graphDirty && draftValidation !== null && !draftValidation.valid) {
      setSaveBlockers([t("workflowEditor.draftValidationBlocksSave")]);
      return;
    }
    setSaving(true);
    setSaveError("");
    setSaveBlockers([]);
    try {
      const metadata = latestDirty.metadataDirty ? workflowEditorDraftMetadata(draftState) : undefined;
      const graph = workflowEditorDraftGraph(draftState);
      const preview =
        confirmedPreview ??
        (await api.previewWorkflowGraphSave({
          expectedVersion: draftState.source.workflow.version,
          graph,
          metadata,
          workflowID,
        }));
      const actionableBlockers = preview.blockers.filter(
        (blocker) => blocker.code !== "confirmation_required",
      );
      if (
        confirmedPreview === undefined &&
        preview.confirmationRequired &&
        actionableBlockers.length === 0
      ) {
        setSaveConfirmationPreview(preview);
        setSaveBlockers([]);
        return;
      }
      if (actionableBlockers.length > 0) {
        setSaveConfirmationPreview(null);
        setSaveBlockers(actionableBlockers.map((blocker) => blocker.message));
        return;
      }
      const saved = await api.saveWorkflowGraph({
        expectedVersion: draftState.source.workflow.version,
        graph,
        metadata,
        workflowID,
        confirmation:
          confirmedPreview === undefined ? undefined : confirmationFromImpact(confirmedPreview.impact),
      });
      if (!saved.saved || saved.definition === null) {
        setSaveConfirmationPreview(saved.confirmationRequired ? saved : null);
        setSaveBlockers(saved.blockers.map((blocker) => blocker.message));
        return;
      }
      setSaveConfirmationPreview(null);
      dispatch({ source: saved.definition, type: "reset" });
      await Promise.all([
        data.workflowQuery.refetch(),
        data.validationQuery.refetch(),
        projectID.length > 0 ? data.boardQuery.refetch() : Promise.resolve(),
      ]);
    } catch (error) {
      setSaveError(errorMessage(error));
    } finally {
      setSaving(false);
    }
  }
}

function WorkflowEditorTopChromeBlur() {
  return (
    <div
      aria-hidden="true"
      className="pointer-events-none fixed inset-x-0 top-0 z-10 h-[calc(var(--native-titlebar-height)*2)]"
      data-testid="workflow-editor-top-chrome-blur"
      style={workflowEditorTopChromeBlurStyle}
    />
  );
}

const workflowEditorTopChromeBlurStyle = {
  WebkitBackdropFilter: "blur(16px) saturate(0.8) brightness(0.78)",
  WebkitMaskImage: "linear-gradient(to bottom, black 0%, black 30%, transparent 100%)",
  background: "color-mix(in srgb, var(--window-glass-tint) 65%, transparent)",
  backdropFilter: "blur(16px) saturate(0.8) brightness(0.78)",
  maskImage: "linear-gradient(to bottom, black 0%, black 30%, transparent 100%)",
} satisfies CSSProperties;

function confirmationFromImpact(impact: WorkflowGraphSaveImpact): WorkflowGraphSaveConfirmation {
  return {
    expectedEdgeTaskReferenceCount: impact.edgeTaskReferenceCount,
    expectedNodeTaskReferenceCount: impact.nodeTaskReferenceCount,
    expectedRemovedEdgeCount: impact.removedEdgeCount,
    expectedRemovedNodeCount: impact.removedNodeCount,
    expectedRemovedTransitionGroupCount: impact.removedTransitionGroupCount,
  };
}

type PendingGraphDelete = Readonly<{
  requestID: string;
  selection: WorkflowGraphSelection;
  summary: WorkflowEditorCascadeSummary;
}>;

type GraphDeletionPlan =
  | Readonly<{ kind: "blocked"; warning: string }>
  | Readonly<{ kind: "ready"; summary: WorkflowEditorCascadeSummary }>;

function planGraphDeletion(
  draft: DraftWorkflowDefinition,
  selection: WorkflowGraphSelection,
): GraphDeletionPlan {
  if (selection.kind === "edge") {
    const mutation = deleteWorkflowEdge(draft, selection.edgeID);
    return graphDeletionPlanFromMutation(mutation.warnings, mutation.summary);
  }
  if (selection.kind === "node") {
    const mutation = deleteWorkflowNode(draft, selection.nodeID);
    return graphDeletionPlanFromMutation(mutation.warnings, mutation.summary);
  }
  const mutation = deleteWorkflowNodeGroup(draft, selection.groupID);
  return graphDeletionPlanFromMutation(mutation.warnings, mutation.summary);
}

function graphDeletionPlanFromMutation(
  warnings: readonly string[],
  summary: WorkflowEditorCascadeSummary,
): GraphDeletionPlan {
  const warning = warnings[0];
  if (warning !== undefined) {
    return { kind: "blocked", warning };
  }
  return { kind: "ready", summary };
}

function dispatchGraphDeletion(
  selection: WorkflowGraphSelection,
  dispatch: (action: WorkflowEditorDraftAction) => void,
): void {
  if (selection.kind === "edge") {
    dispatch({ edgeID: selection.edgeID, type: "deleteEdge" });
    return;
  }
  if (selection.kind === "node") {
    dispatch({ nodeID: selection.nodeID, type: "deleteNode" });
    return;
  }
  dispatch({ groupID: selection.groupID, type: "deleteNodeGroup" });
}

function cascadeRowCount(summary: WorkflowEditorCascadeSummary): number {
  return (
    summary.removedNodeIDs.length +
    summary.removedEdgeIDs.length +
    summary.removedTransitionGroupIDs.length
  );
}

function cascadeSummaryEquals(
  left: WorkflowEditorCascadeSummary,
  right: WorkflowEditorCascadeSummary,
): boolean {
  return (
    stringListEquals(left.removedNodeIDs, right.removedNodeIDs) &&
    stringListEquals(left.removedEdgeIDs, right.removedEdgeIDs) &&
    stringListEquals(left.removedTransitionGroupIDs, right.removedTransitionGroupIDs)
  );
}

function stringListEquals(left: readonly string[], right: readonly string[]): boolean {
  if (left.length !== right.length) {
    return false;
  }
  return left.every((value, index) => value === right[index]);
}

function nextGraphDeleteRequestID(workflowID: string, indexRef: { current: number }): string {
  indexRef.current += 1;
  return `${workflowID}-delete-${indexRef.current.toString()}`;
}

function deleteWarningTranslationKey(warning: string): string {
  if (warning === workflowEditorGraphMutationWarnings.startNodeDelete) {
    return "workflowEditor.startNodeDeleteBlocked";
  }
  if (warning === workflowEditorGraphMutationWarnings.lastTerminalDelete) {
    return "workflowEditor.lastTerminalDeleteBlocked";
  }
  return "workflowEditor.deleteBlockedGeneric";
}

async function copyWorkflowNodeText(
  value: string,
  nativeBridge: ReturnType<typeof useAppServices>["nativeBridge"],
): Promise<void> {
  if (nativeBridge.capabilities.clipboard.writeText) {
    await nativeBridge.clipboard.writeText(value);
    return;
  }
  await navigator.clipboard.writeText(value);
}

function WorkflowEditorLegendIsland() {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(true);
  return (
    <FloatingNoticeIsland
      collapsed={collapsed}
      collapseLabel={t("app.collapse")}
      expandedClassName="floating-notice-expanded grid h-[204px] w-[min(300px,calc(100vw-var(--space-2)*2))] gap-[6px] overflow-hidden rounded-[var(--radius-xl)] p-[var(--space-2)]"
      expandLabel={t("app.expand")}
      icon={
        <CircleQuestionMark
          aria-hidden="true"
          data-testid="workflow-legend-collapsed-help-icon"
          size={24}
          strokeWidth={1.7}
        />
      }
      level={3}
      onCollapsedChange={setCollapsed}
      positionClassName="left-[var(--space-2)] bottom-[var(--space-2)]"
      title={t("workflowEditor.legend")}
      tone="neutral"
    >
      <div className="grid gap-[6px] pt-[4px] text-sm leading-none text-[var(--color-on-island)]">
        <LegendRow label={t("workflowEditor.legendContinueSession")}>
          <EdgeLegendSwatch tone="neutral" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendFreshSession")}>
          <EdgeLegendSwatch tone="primary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendCompactSession")}>
          <EdgeLegendSwatch tone="secondary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendAgentNode")}>
          <NodeLegendSwatch tone="neutral" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendTerminalState")}>
          <NodeLegendSwatch tone="success" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendStartingState")}>
          <NodeLegendSwatch tone="primary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendMultiAgentJoin")}>
          <NodeLegendSwatch shape="diamond" tone="secondary" />
        </LegendRow>
      </div>
    </FloatingNoticeIsland>
  );
}

function LegendRow({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <div className="grid grid-cols-[26px_minmax(0,1fr)] items-center gap-[var(--space-2)]">
      <span className="grid h-3 place-items-center">{children}</span>
      <span className="min-w-0">{label}</span>
    </div>
  );
}

function EdgeLegendSwatch({ tone }: Readonly<{ tone: "neutral" | "primary" | "secondary" }>) {
  return (
    <svg
      aria-hidden="true"
      className={edgeLegendToneClassName(tone)}
      data-testid="workflow-legend-edge-swatch"
      fill="none"
      height="6"
      viewBox="0 0 22 6"
      width="22"
    >
      <path
        d="M1 3H19"
        data-testid="workflow-legend-edge-line"
        stroke="currentColor"
        strokeLinecap="round"
        strokeWidth="1.25"
      />
      <path
        d="M17 1L20 3L17 5"
        data-testid="workflow-legend-edge-head"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.25"
      />
    </svg>
  );
}

function NodeLegendSwatch({
  shape = "box",
  tone,
}: Readonly<{ shape?: "box" | "diamond"; tone: "neutral" | "primary" | "secondary" | "success" }>) {
  const shapeClassName =
    shape === "diamond" ? "h-[10px] w-[10px] rotate-45 rounded-[2px]" : "h-[9px] w-[14px] rounded-[2px]";
  return (
    <span
      aria-hidden="true"
      className={cx("block border bg-[var(--color-island-1)]", shapeClassName, nodeLegendToneClassName(tone))}
      data-testid="workflow-legend-node-swatch"
    />
  );
}

function edgeLegendToneClassName(tone: "neutral" | "primary" | "secondary"): string {
  if (tone === "primary") {
    return "text-[var(--color-primary)]";
  }
  if (tone === "secondary") {
    return "text-[var(--color-secondary)]";
  }
  return "text-[var(--color-muted)]";
}

function nodeLegendToneClassName(tone: "neutral" | "primary" | "secondary" | "success"): string {
  if (tone === "primary") {
    return "border-[var(--color-primary)]";
  }
  if (tone === "secondary") {
    return "border-[var(--color-secondary)]";
  }
  if (tone === "success") {
    return "border-[var(--color-success)]";
  }
  return "border-[var(--color-outline)]";
}

function workflowEditorDraftStateReducer(
  state: WorkflowEditorDraftState | null,
  action: Parameters<typeof workflowEditorDraftReducer>[1],
): WorkflowEditorDraftState | null {
  if (state === null) {
    return action.type === "reset" ? initializeWorkflowEditorDraft(action.source) : state;
  }
  return workflowEditorDraftReducer(state, action);
}

type WorkflowSaveConfirmationPreviewEntry = Readonly<{
  key: string;
  preview: WorkflowGraphSavePreview;
}>;

function workflowSaveConfirmationPreviewKey(state: WorkflowEditorDraftState): string {
  return [
    state.source.workflow.id,
    state.source.workflow.version.toString(),
    state.version.toString(),
  ].join(":");
}

function useWorkflowDraftValidationQuery(workflowID: string, draftState: WorkflowEditorDraftState | null) {
  const { api } = useAppServices();
  return useQuery({
    queryKey: queryKeys.workflowDraftValidation(
      workflowID,
      draftState?.source.workflow.version ?? 0,
      draftState?.version ?? 0,
    ),
    queryFn: async () => {
      if (draftState === null) {
        throw new Error("Workflow draft validation requested before draft is initialized.");
      }
      return api.validateWorkflowGraphDraft({
        graph: workflowEditorDraftGraph(draftState),
        metadata: workflowEditorDraftMetadata(draftState),
        modes: ["draft", "execution"],
        workflowID,
      });
    },
    enabled: draftState !== null,
  });
}

function useWorkflowGraphLayoutQuery(
  workflowID: string,
  definition: WorkflowDefinition | undefined,
  draftVersion: number,
  validation: WorkflowValidation | null,
): UseQueryResult<WorkflowGraphLayout> {
  return useQuery({
    queryKey: queryKeys.workflowGraphLayout(
      workflowID,
      (definition?.workflow.version ?? 0) * 100_000 + draftVersion,
      validation?.valid ?? false,
      validation?.errors ?? [],
    ),
    queryFn: async () => {
      if (definition === undefined || validation === null) {
        throw new Error("Workflow graph layout requested before workflow definition and validation loaded.");
      }
      return layoutWorkflowGraph(definition, validation);
    },
    enabled: definition !== undefined && validation !== null,
    placeholderData: (previous) => previous,
  });
}

function WorkflowEditorStatusIsland({
  confirmationPreview,
  controller,
  onCancelConfirmation,
  onConfirmSave,
  onDiscard,
}: Readonly<{
  confirmationPreview: WorkflowGraphSavePreview | null;
  controller: WorkflowEditorDraftController;
  onCancelConfirmation: () => void;
  onConfirmSave: () => void;
  onDiscard: () => void;
}>) {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(false);
  const validationErrors = [
    ...(controller.draftValidation?.errors ?? []),
    ...(controller.executionValidation?.errors ?? []),
  ];
  const hasIssues =
    validationErrors.length > 0 || controller.saveBlockers.length > 0 || controller.saveError.length > 0;
  if (!controller.dirty.dirty && controller.state.conflict === null && !hasIssues) {
    return null;
  }
  return (
    <FloatingNoticeIsland
      collapsed={collapsed}
      collapseLabel={t("app.collapse")}
      expandedClassName="floating-notice-expanded grid max-h-[min(400px,calc(100vh-32px))] w-[min(400px,calc(100vw-32px))] gap-[6px] overflow-y-auto overflow-x-hidden rounded-[var(--radius-xl)] p-[var(--space-3)]"
      expandLabel={t("app.expand")}
      level={3}
      onCollapsedChange={setCollapsed}
      positionClassName="right-[var(--space-4)] bottom-[var(--space-4)]"
      title={controller.dirty.dirty ? t("workflowEditor.unsavedChanges") : t("board.workflowIssues")}
      tone={
        controller.draftValidation?.valid === false || controller.saveError.length > 0 ? "danger" : "neutral"
      }
    >
      <div className="grid gap-[var(--space-3)] pt-[6px]">
        {confirmationPreview !== null ? (
          <WorkflowSaveConfirmation
            disabled={controller.saving}
            impact={confirmationPreview.impact}
            onCancel={onCancelConfirmation}
            onConfirm={onConfirmSave}
          />
        ) : controller.dirty.dirty ? (
          <div className="grid grid-cols-2 gap-[var(--space-2)]">
            <Button className="w-full" disabled={controller.saving} onClick={onDiscard} variant="danger">
              {t("workflowEditor.discard")}
            </Button>
            <Button
              className="w-full"
              disabled={
                controller.saving ||
                (controller.dirty.graphDirty && controller.draftValidation?.valid === false)
              }
              onClick={controller.save}
              variant="primary"
            >
              {controller.saving ? t("workflowEditor.saving") : t("workflowEditor.save")}
            </Button>
          </div>
        ) : null}
        {controller.state.conflict !== null ? (
          <div className="grid gap-[var(--space-2)]">
            <p className="m-0 text-sm text-[var(--color-on-island)]">{t("workflowEditor.remoteConflict")}</p>
            <div className="flex flex-wrap gap-[var(--space-2)]">
              <Button
                onClick={() => {
                  controller.dispatch({ type: "reloadConflict" });
                }}
                variant="secondary"
              >
                {t("workflowEditor.reloadRemote")}
              </Button>
              <Button
                onClick={() => {
                  controller.dispatch({ type: "keepEditing" });
                }}
                variant="ghost"
              >
                {t("workflowEditor.keepEditing")}
              </Button>
            </div>
          </div>
        ) : null}
        {controller.saveError.length > 0 ? (
          <p className="m-0 text-sm text-[var(--color-error)]">{controller.saveError}</p>
        ) : null}
        {controller.saveBlockers.length > 0 ? (
          <ul className="m-0 grid gap-[var(--space-1)] p-0">
            {controller.saveBlockers.map((blocker) => (
              <li className="list-none text-sm text-[var(--color-error)]" key={blocker}>
                {blocker}
              </li>
            ))}
          </ul>
        ) : null}
        {controller.draftValidation !== null && controller.draftValidation.errors.length > 0 ? (
          <WorkflowValidationIssues errors={controller.draftValidation.errors} />
        ) : null}
        {controller.executionValidation !== null && controller.executionValidation.errors.length > 0 ? (
          <WorkflowValidationIssues errors={controller.executionValidation.errors} />
        ) : null}
      </div>
    </FloatingNoticeIsland>
  );
}

function WorkflowSaveConfirmation({
  disabled,
  impact,
  onCancel,
  onConfirm,
}: Readonly<{
  disabled: boolean;
  impact: WorkflowGraphSaveImpact;
  onCancel: () => void;
  onConfirm: () => void;
}>) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-[var(--space-3)]">
      <p className="m-0 text-sm text-[var(--color-on-island)]">
        {t("workflowEditor.destructiveSaveWarning")}
      </p>
      <ul className="m-0 grid gap-[var(--space-1)] p-0 text-sm text-[var(--color-muted)]">
        <li className="list-none">
          {t("workflowEditor.saveImpactNodes", { count: impact.removedNodeCount })}
        </li>
        <li className="list-none">
          {t("workflowEditor.saveImpactTransitionGroups", {
            count: impact.removedTransitionGroupCount,
          })}
        </li>
        <li className="list-none">
          {t("workflowEditor.saveImpactEdges", { count: impact.removedEdgeCount })}
        </li>
      </ul>
      <div className="grid grid-cols-2 gap-[var(--space-2)]">
        <Button className="w-full" disabled={disabled} onClick={onCancel} variant="secondary">
          {t("app.cancel")}
        </Button>
        <Button className="w-full" disabled={disabled} onClick={onConfirm} variant="danger">
          {disabled ? t("workflowEditor.saving") : t("workflowEditor.confirmSave")}
        </Button>
      </div>
    </div>
  );
}

function emptyWorkflowDefinition(workflowID: string): WorkflowDefinition {
  return {
    edges: [],
    nodeGroups: [],
    nodes: [],
    transitionGroups: [],
    workflow: { description: "", version: 1, id: workflowID, name: "" },
    derivedWiring: emptyWorkflowDerivedWiring,
  };
}

type WorkflowEditorViewState =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ error: Error; kind: "linkError" }>
  | Readonly<{ kind: "unlinked" }>
  | Readonly<{ error: Error; kind: "loadError" }>
  | Readonly<{ graph: WorkflowGraphLayout; kind: "ready"; validation: WorkflowValidation }>;

function workflowEditorViewState(
  data: WorkflowEditorData,
  layoutQuery: UseQueryResult<WorkflowGraphLayout>,
): WorkflowEditorViewState {
  if (isLinkGateLoading(data)) {
    return { kind: "loading" };
  }
  if (data.linksQuery.isError) {
    return { error: data.linksQuery.error, kind: "linkError" };
  }
  if (!data.linked) {
    return { kind: "unlinked" };
  }
  if (isGraphLoading(data, layoutQuery)) {
    return { kind: "loading" };
  }
  const loadError = workflowEditorLoadError(data, layoutQuery);
  if (loadError !== null) {
    return { error: loadError, kind: "loadError" };
  }
  if (layoutQuery.data === undefined || data.validationQuery.data === undefined) {
    return { kind: "loading" };
  }
  return { graph: layoutQuery.data, kind: "ready", validation: data.validationQuery.data };
}

function isLinkGateLoading(data: WorkflowEditorData): boolean {
  return data.projectContext && (data.linksQuery.isPending || data.boardQuery.isPending);
}

function isGraphLoading(data: WorkflowEditorData, layoutQuery: UseQueryResult<WorkflowGraphLayout>): boolean {
  return data.workflowQuery.isPending || data.validationQuery.isPending || layoutQuery.isPending;
}

function workflowEditorLoadError(
  data: WorkflowEditorData,
  layoutQuery: UseQueryResult<WorkflowGraphLayout>,
): Error | null {
  return (
    data.boardQuery.error ??
    data.workflowQuery.error ??
    data.validationQuery.error ??
    layoutQuery.error ??
    null
  );
}
