/* eslint-disable complexity, max-lines -- The route coordinates data loading, draft lifecycle, save, and floating islands. */
import { useEffect, useMemo, useReducer, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

import { emptyWorkflowDerivedWiring, type WorkflowDefinition, type WorkflowValidation } from "../../api";
import { errorMessage } from "../../api/errors";
import { useSidebar } from "../../app/sidebarContext";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import { useWindowChromeTitle } from "../../app/windowChromeTitle";
import { Button, ErrorState, FloatingNoticeIsland, LoadingState } from "../../ui";
import { chromeContentPaddingClassName } from "../../ui/chromePadding";
import { cx } from "../../ui/classes";
import { WorkflowValidationIssues } from "../workflow/WorkflowValidationIssues";
import { WorkflowGraphCanvas } from "./WorkflowGraphCanvas";
import { layoutWorkflowGraph, type WorkflowGraphLayout } from "./workflowGraphLayout";
import { useWorkflowEditorData, type WorkflowEditorData } from "./useWorkflowEditorData";
import {
  initializeWorkflowEditorDraft,
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

export type WorkflowEditorRouteProps = Readonly<{
  projectID: string;
  workflowID: string;
}>;

export function WorkflowEditorRoute({ projectID, workflowID }: WorkflowEditorRouteProps) {
  const { t } = useTranslation();
  const { api, nativeBridge } = useAppServices();
  const { openSidebar } = useSidebar();
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

  return (
    <section
      className={`h-full min-h-0 w-full ${chromeContentPaddingClassName}`}
      data-testid="workflow-editor-route"
    >
      <WorkflowGraphCanvas
        graph={viewState.graph}
        onCopyText={async (value) => copyWorkflowNodeText(value, nativeBridge)}
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
        controller={controller}
        onDiscard={() => {
          dispatch({ source: controller.state.source, type: "reset" });
        }}
      />
      <WorkflowEditorLegendIsland />
    </section>
  );

  async function saveWorkflowDraft(): Promise<void> {
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
      const preview = await api.previewWorkflowGraphSave({
        expectedVersion: draftState.source.workflow.version,
        graph: workflowEditorDraftGraph(draftState),
        metadata,
        workflowID,
      });
      if (preview.blockers.length > 0 || preview.confirmationRequired) {
        setSaveBlockers(preview.blockers.map((blocker) => blocker.message));
        return;
      }
      const saved = await api.saveWorkflowGraph({
        expectedVersion: draftState.source.workflow.version,
        graph: workflowEditorDraftGraph(draftState),
        metadata,
        workflowID,
      });
      if (!saved.saved || saved.definition === null) {
        setSaveBlockers(saved.blockers.map((blocker) => blocker.message));
        return;
      }
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
  const [collapsed, setCollapsed] = useState(false);
  return (
    <FloatingNoticeIsland
      collapsed={collapsed}
      collapseLabel={t("app.collapse")}
      expandedClassName="floating-notice-expanded grid w-[min(300px,calc(100vw-32px))] gap-[6px] overflow-hidden rounded-[var(--radius-xl)] p-[var(--space-3)]"
      expandLabel={t("app.expand")}
      onCollapsedChange={setCollapsed}
      positionClassName="left-[var(--space-4)] bottom-[var(--space-4)]"
      title={t("workflowEditor.legend")}
      tone="neutral"
    >
      <div className="grid gap-[var(--space-2)] pt-[6px] text-sm text-[var(--color-on-island)]">
        <LegendRow label={t("workflowEditor.legendCompactSession")}>
          <EdgeLegendSwatch tone="secondary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendFreshSession")}>
          <EdgeLegendSwatch tone="primary" />
        </LegendRow>
        <LegendRow label={t("workflowEditor.legendContinueSession")}>
          <EdgeLegendSwatch tone="neutral" />
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
        <LegendRow label={t("workflowEditor.legendAgentNode")}>
          <NodeLegendSwatch tone="neutral" />
        </LegendRow>
      </div>
    </FloatingNoticeIsland>
  );
}

function LegendRow({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <div className="grid grid-cols-[52px_minmax(0,1fr)] items-center gap-[var(--space-2)]">
      <span className="grid h-6 place-items-center">{children}</span>
      <span className="min-w-0">{label}</span>
    </div>
  );
}

function EdgeLegendSwatch({ tone }: Readonly<{ tone: "neutral" | "primary" | "secondary" }>) {
  return (
    <svg
      aria-hidden="true"
      className={edgeLegendToneClassName(tone)}
      fill="none"
      height="12"
      viewBox="0 0 44 12"
      width="44"
    >
      <path d="M2 6H38" stroke="currentColor" strokeLinecap="round" strokeWidth="2.5" />
      <path
        d="M34 2L40 6L34 10"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="2.5"
      />
    </svg>
  );
}

function NodeLegendSwatch({
  shape = "box",
  tone,
}: Readonly<{ shape?: "box" | "diamond"; tone: "neutral" | "primary" | "secondary" | "success" }>) {
  return (
    <span
      aria-hidden="true"
      className={cx(
        "block h-[18px] w-[28px] rounded-[var(--radius-s)] border-2 bg-[var(--color-island-1)]",
        shape === "diamond" && "h-[20px] w-[20px] rotate-45",
        nodeLegendToneClassName(tone),
      )}
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
  return "text-[var(--color-outline)]";
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
  controller,
  onDiscard,
}: Readonly<{
  controller: WorkflowEditorDraftController;
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
      onCollapsedChange={setCollapsed}
      positionClassName="right-[var(--space-4)] bottom-[var(--space-4)]"
      title={controller.dirty.dirty ? t("workflowEditor.unsavedChanges") : t("board.workflowIssues")}
      tone={
        controller.draftValidation?.valid === false || controller.saveError.length > 0 ? "danger" : "neutral"
      }
    >
      <div className="grid gap-[var(--space-3)] pt-[6px]">
        {controller.dirty.dirty ? (
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
