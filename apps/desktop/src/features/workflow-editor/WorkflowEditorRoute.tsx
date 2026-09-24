import { useCallback, useState, type Dispatch, type ReactNode, type SetStateAction } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useAtomValue, useAtomSuspense, useAtomRefresh } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";

import { errorMessage, isProjectMissingError, type WorkflowGraphSavePreview } from "@/api";
import type { SidebarPageNavigator } from "@/app-facade";
import {
  SidebarRootOwner,
  useOwnedSidebarRoots,
  useAppServices,
  sidebarTitle,
  sidebarSizePreference,
  type SidebarRootController,
} from "@/app-facade";
import { useSidebarBackWhen } from "@/app-facade";
import type { WorkflowInspectorInitialFocus, WorkflowInspectorSelection } from "@/app-facade";
import { useStatusController } from "@/app-facade";
import { usePublishSidebarHeaderAction } from "@/app-facade";
import { useWindowChromeTitle } from "@/app-facade";
import { ErrorState, LoadingState } from "@/ui";
import { cx } from "@/ui";
import { type WorkflowEditorDraftState } from "./workflowEditorDraft";
import {
  createWorkflowEditorViewModel,
  useWorkflowEditorActions,
  useWorkflowGraphEditorActions,
  type WorkflowEditorViewModel,
} from "./WorkflowEditorViewModel";
import {
  useWorkflowEditorView,
  useWorkflowEditorDraftView,
  type WorkflowEditorView,
} from "./useWorkflowEditorView";
import { type PendingGraphMutation } from "./workflowEditorGraphMutationPlanning";
import { workflowEditorViewState } from "./workflowEditorViewState";
import { useWorkflowGraphDeleteConfirmation } from "./useWorkflowGraphDeleteConfirmation";
import { WorkflowEditorCanvas } from "./WorkflowEditorCanvas";
import { WorkflowDraftDetails } from "./WorkflowDraftInspector";
import { WorkflowDeleteButton } from "./WorkflowDeleteButton";
import { WorkflowEditorEmbeddedInspector } from "./WorkflowEditorEmbeddedInspector";
import { WorkflowEditableInspectorDestination } from "./WorkflowInspectorSidebar";
import { WorkflowEditorLegendIsland } from "./WorkflowEditorLegendIsland";
import { WorkflowEditorStatusIsland } from "./WorkflowEditorStatusIsland";
import type { WorkflowGraphSelection } from "./workflowGraphSelection";
import type { WorkflowGraphLayout } from "./workflowGraphLayout";
import type { WorkflowEditorDraftAction } from "./workflowEditorDraft";

export type WorkflowEditorRouteProps = Readonly<{
  navigator?: SidebarPageNavigator | undefined;
  projectID: string;
  surface?: "route" | "settings" | "sidebar" | undefined;
  workflowID: string;
}>;

type WorkflowGraphEditorRouteProps = Omit<WorkflowEditorRouteProps, "surface"> &
  Readonly<{ surface?: "route" | "sidebar" | undefined }>;

type WorkflowEditorEmbeddedInspectorSelection = Readonly<{
  initialFocus?: WorkflowInspectorInitialFocus | undefined;
  selection: WorkflowInspectorSelection;
  workflowID: string;
}>;

type WorkflowEditorReadyViewProps = Readonly<{
  activeEmbeddedInspectorInitialFocus?: WorkflowInspectorInitialFocus | undefined;
  activeEmbeddedInspectorSelection: WorkflowInspectorSelection | null;
  closeDeletedNodeInspector: (selection: WorkflowGraphSelection) => void;
  controller: WorkflowEditorView;
  model: WorkflowEditorViewModel;
  deleteConfirmationDialog: ReactNode;
  dispatch: (action: WorkflowEditorDraftAction) => void;
  draftState: WorkflowEditorDraftState | null;
  graph: WorkflowGraphLayout;
  inspect: (selection: WorkflowInspectorSelection, initialFocus?: WorkflowInspectorInitialFocus) => void;
  onClearEmbeddedInspector: () => void;
  openDeleteConfirmation: (mutation: PendingGraphMutation) => void;
  save: ReturnType<typeof useWorkflowEditorActions>;
  confirmationPreview: WorkflowGraphSavePreview | null;
  surface: "route" | "sidebar";
  workflowID: string;
}>;

export function WorkflowEditorRoute(props: WorkflowEditorRouteProps) {
  const key = JSON.stringify([props.workflowID, props.projectID.trim(), props.surface]);
  if (props.surface === "settings") {
    return <WorkflowSettingsRoute key={key} {...props} />;
  }
  if (props.surface === "sidebar") {
    return <WorkflowEditorRouteContent key={key} {...props} surface="sidebar" openSidebar={null} />;
  }
  return (
    <SidebarRootOwner key={key}>
      <OwnedWorkflowEditorRoute {...props} surface="route" />
    </SidebarRootOwner>
  );
}

function WorkflowSettingsRoute({ navigator, projectID, workflowID }: WorkflowEditorRouteProps) {
  const { t } = useTranslation();
  const model = useWorkflowEditorModel(projectID, workflowID);
  const data = useAtomValue(model.data);
  const controller = useWorkflowEditorDraftView(model);
  const save = useWorkflowEditorActions(model);
  const { confirmationPreview } = useAtomValue(model.saveState);

  if (data.workflowQuery.isError) {
    return (
      <ErrorState
        body={errorMessage(data.workflowQuery.error)}
        onRetry={() => {
          save.retryLoad(undefined);
        }}
        retryLabel={t("app.retry")}
        title={t("workflowEditor.loadFailed")}
      />
    );
  }
  if (data.workflowQuery.isPending || controller === null) {
    return <LoadingState appearanceDelayMs={0} title={t("workflowEditor.loadingTitle")} />;
  }
  return (
    <>
      <WorkflowEditorObservationFailures model={model} />
      <WorkflowSettingsSurface
        controller={controller}
        navigator={navigator}
        save={save}
        confirmationPreview={confirmationPreview}
        workflowID={workflowID}
      />
    </>
  );
}

function useWorkflowEditorModel(rawProjectID: string, workflowID: string) {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const { push } = useStatusController();
  const [model] = useState(() =>
    createWorkflowEditorViewModel({
      services,
      client,
      workflowID,
      projectID: rawProjectID.trim() || null,
      t,
      push,
    }),
  );
  return model;
}

function OwnedWorkflowEditorRoute(props: WorkflowGraphEditorRouteProps) {
  const { open } = useOwnedSidebarRoots();
  return <WorkflowEditorRouteContent {...props} openSidebar={open} />;
}

function WorkflowEditorRouteContent({
  openSidebar,
  navigator,
  projectID,
  surface = "route",
  workflowID,
}: WorkflowGraphEditorRouteProps & Readonly<{ openSidebar: SidebarRootController["open"] | null }>) {
  const { t } = useTranslation();
  const model = useWorkflowEditorModel(projectID, workflowID);
  const data = useAtomValue(model.data);
  const controller = useWorkflowEditorView(model);
  const save = useWorkflowGraphEditorActions(model);
  const { confirmationPreview } = useAtomValue(model.saveState);
  const { draftState } = useAtomValue(model.state);
  const dispatch = save.edit;
  useSidebarBackWhen(data.linksQuery.isError && isProjectMissingError(data.linksQuery.error), navigator);
  const workflow = data.workflowQuery.data?.workflow;
  const [embeddedInspectorSelection, setEmbeddedInspectorSelection] =
    useState<WorkflowEditorEmbeddedInspectorSelection | null>(null);
  const graphState = useAtomValue(model.graph);
  const { layoutQuery } = graphState;
  useWindowChromeTitle(
    workflow === undefined ? t("workflowEditor.title") : workflow.name,
    surface === "route",
  );

  const inspectWorkflowGraphItem = useWorkflowGraphInspector({
    openSidebar,
    setEmbeddedInspectorSelection,
    surface,
    workflowID,
    model,
  });

  const closeDeletedNodeInspector = useCallback(
    (selection: WorkflowGraphSelection) => {
      if (selection.kind !== "node") {
        return;
      }
      if (
        surface === "sidebar" &&
        embeddedSelectionMatchesNode(embeddedInspectorSelection, workflowID, selection.nodeID)
      ) {
        setEmbeddedInspectorSelection(null);
      }
    },
    [embeddedInspectorSelection, surface, workflowID],
  );

  const deleteConfirmation = useWorkflowGraphDeleteConfirmation({
    closeDeletedNodeInspector,
    dispatch,
    draftState,
    workflowID,
  });

  const viewState = workflowEditorViewState(data, layoutQuery, graphState.projectedGraph);
  const activeEmbeddedInspector = embeddedInspectorForWorkflow(
    embeddedInspectorSelection,
    surface,
    workflowID,
  );

  if (viewState.kind !== "ready") {
    return (
      <WorkflowEditorNonReadyState
        onRetryLinks={() => {
          save.retryLinks(undefined);
        }}
        onRetryLoad={() => {
          save.retryLoad(undefined);
          save.retryLayout(undefined);
        }}
        viewState={viewState}
      />
    );
  }
  if (controller === null)
    return <LoadingState appearanceDelayMs={0} title={t("workflowEditor.loadingTitle")} />;

  return (
    <>
      <WorkflowEditorObservationFailures model={model} />
      <WorkflowEditorReadyView
        activeEmbeddedInspectorInitialFocus={activeEmbeddedInspector?.initialFocus}
        activeEmbeddedInspectorSelection={activeEmbeddedInspector?.selection ?? null}
        closeDeletedNodeInspector={closeDeletedNodeInspector}
        controller={controller}
        model={model}
        deleteConfirmationDialog={deleteConfirmation.dialog}
        dispatch={dispatch}
        draftState={draftState}
        graph={viewState.graph}
        inspect={inspectWorkflowGraphItem}
        onClearEmbeddedInspector={() => {
          setEmbeddedInspectorSelection(null);
        }}
        openDeleteConfirmation={deleteConfirmation.open}
        save={save}
        confirmationPreview={confirmationPreview}
        surface={surface}
        workflowID={workflowID}
      />
    </>
  );
}

function WorkflowEditorObservationFailures({ model }: Readonly<{ model: WorkflowEditorViewModel }>) {
  const { t } = useTranslation();
  const workflowError = useAtomSuspense(model.workflowObservation).value;
  const projectError = useAtomSuspense(model.projectObservation).value;
  const retryWorkflow = useAtomRefresh(model.workflowObservation);
  const retryProject = useAtomRefresh(model.projectObservation);
  const observations = [
    { key: "workflow", error: workflowError, retry: retryWorkflow },
    { key: "project", error: projectError, retry: retryProject },
  ];
  return (
    <>
      {observations.map((observation) => {
        return observation.error === null ? null : (
          <ErrorState
            key={observation.key}
            fullPage={false}
            body={errorMessage(observation.error)}
            title={t("workflowEditor.loadFailed")}
            onRetry={observation.retry}
            retryLabel={t("app.retry")}
          />
        );
      })}
    </>
  );
}

function useWorkflowGraphInspector({
  openSidebar,
  setEmbeddedInspectorSelection,
  surface,
  workflowID,
  model,
}: Readonly<{
  openSidebar: SidebarRootController["open"] | null;
  setEmbeddedInspectorSelection: Dispatch<SetStateAction<WorkflowEditorEmbeddedInspectorSelection | null>>;
  surface: "route" | "sidebar";
  workflowID: string;
  model: WorkflowEditorViewModel;
}>): (selection: WorkflowInspectorSelection, initialFocus?: WorkflowInspectorInitialFocus) => void {
  const { t } = useTranslation();
  return useCallback(
    (selection, initialFocus) => {
      if (surface === "sidebar") {
        setEmbeddedInspectorSelection({ initialFocus, selection, workflowID });
        return;
      }
      if (openSidebar === null) {
        throw new Error("Route Workflow inspection requires a sidebar root owner.");
      }
      const destination = {
        initialFocus,
        kind: "workflowInspect",
        mode: "overlay",
        selection,
        workflowID,
      } as const;
      openSidebar({
        kind: "custom",
        mode: "overlay",
        title: sidebarTitle(destination, t),
        sizing: sidebarSizePreference(destination),
        content: (navigator) => (
          <WorkflowEditableInspectorDestination
            model={model}
            navigator={navigator}
            initialFocus={initialFocus}
            selection={selection}
            workflowID={workflowID}
          />
        ),
      });
    },
    [openSidebar, setEmbeddedInspectorSelection, surface, workflowID, model, t],
  );
}

function embeddedInspectorForWorkflow(
  embedded: WorkflowEditorEmbeddedInspectorSelection | null,
  surface: "route" | "sidebar",
  workflowID: string,
): WorkflowEditorEmbeddedInspectorSelection | null {
  return surface === "sidebar" && embedded?.workflowID === workflowID ? embedded : null;
}

function WorkflowEditorReadyView(props: WorkflowEditorReadyViewProps) {
  const {
    activeEmbeddedInspectorInitialFocus,
    activeEmbeddedInspectorSelection,
    closeDeletedNodeInspector,
    controller,
    model,
    deleteConfirmationDialog,
    dispatch,
    draftState,
    graph,
    inspect,
    onClearEmbeddedInspector,
    openDeleteConfirmation,
    save,
    confirmationPreview,
    surface,
    workflowID,
  } = props;
  const positionStrategy = surface === "route" ? "fixed" : "absolute";
  const editorRoute = (
    <section
      className={cx(
        "app-region-no-drag min-h-0 overflow-hidden",
        surface === "route"
          ? "fixed inset-0 z-0 h-screen w-screen"
          : "relative h-full w-full rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)]",
      )}
      data-testid="workflow-editor-route"
    >
      <WorkflowEditorCanvas
        closeDeletedNodeInspector={closeDeletedNodeInspector}
        dispatch={dispatch}
        draftState={draftState}
        graph={graph}
        inspect={inspect}
        openDeleteConfirmation={openDeleteConfirmation}
        surface={surface}
      />
      <WorkflowEditorEmbeddedInspector
        model={model}
        initialFocus={activeEmbeddedInspectorInitialFocus}
        onClose={onClearEmbeddedInspector}
        selection={activeEmbeddedInspectorSelection}
        workflowID={workflowID}
      />
      <WorkflowEditorStatusIsland
        confirmationPreview={confirmationPreview}
        controller={controller}
        onCancelConfirmation={() => {
          save.dismissConfirmation(undefined);
        }}
        onConfirmSave={() => {
          if (confirmationPreview === null) {
            return;
          }
          save.save(confirmationPreview);
        }}
        onDiscard={() => {
          save.discard(undefined);
        }}
        positionStrategy={positionStrategy}
      />
      {deleteConfirmationDialog}
      <WorkflowEditorLegendIsland positionStrategy={positionStrategy} />
    </section>
  );

  return surface === "route" ? createPortal(editorRoute, document.body) : editorRoute;
}

function WorkflowSettingsSurface({
  controller,
  navigator,
  save,
  confirmationPreview,
  workflowID,
}: Readonly<{
  controller: WorkflowEditorView;
  navigator?: SidebarPageNavigator | undefined;
  save: ReturnType<typeof useWorkflowEditorActions>;
  confirmationPreview: WorkflowGraphSavePreview | null;
  workflowID: string;
}>) {
  usePublishSidebarHeaderAction(
    <WorkflowDeleteButton onDeleted={navigator?.close} workflowID={workflowID} />,
  );
  return (
    <div className="relative h-full min-h-0">
      <div className="min-h-0 overflow-y-auto p-[var(--space-3)]">
        <WorkflowDraftDetails controller={controller} />
      </div>
      <WorkflowEditorStatusIsland
        confirmationPreview={confirmationPreview}
        controller={controller}
        onCancelConfirmation={() => {
          save.dismissConfirmation(undefined);
        }}
        onConfirmSave={() => {
          if (confirmationPreview !== null) {
            save.save(confirmationPreview);
          }
        }}
        onDiscard={() => {
          save.discard(undefined);
        }}
        positionStrategy="absolute"
      />
    </div>
  );
}

function embeddedSelectionMatchesNode(
  embedded: WorkflowEditorEmbeddedInspectorSelection | null,
  workflowID: string,
  nodeID: string,
): boolean {
  return (
    embedded?.workflowID === workflowID &&
    embedded.selection.kind === "node" &&
    embedded.selection.nodeID === nodeID
  );
}

function WorkflowEditorNonReadyState({
  onRetryLinks,
  onRetryLoad,
  viewState,
}: Readonly<{
  onRetryLinks: () => void;
  onRetryLoad: () => void;
  viewState: Exclude<ReturnType<typeof workflowEditorViewState>, { kind: "ready" }>;
}>) {
  const { t } = useTranslation();
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
        onRetry={onRetryLinks}
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
  return (
    <ErrorState
      body={errorMessage(viewState.error)}
      chromePadding
      contentWidth="full"
      onRetry={onRetryLoad}
      retryLabel={t("app.retry")}
      title={t("workflowEditor.loadFailed")}
    />
  );
}
