import { DragDropSurface } from "@app/ui-kit";
import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { hasSelectedWorkflow, type BoardColumn, type SelectedWorkflowBoard } from "@/api";
import { errorMessage } from "@/api";
import { SidebarRootOwner } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { useNativeDialogFallback } from "@/app-facade";
import { reportNonCancelledError, useStatusController } from "@/app-facade";
import { useWindowChromeTitle } from "@/app-facade";
import {
  TaskInitiatingActionDialogs,
  startTaskInitiatingAction,
  type TaskInitiatingActionDialogResult,
  resumeTaskInitiatingAction,
  moveTaskInitiatingAction,
} from "@/shared/execution-target";
import { ProjectLabelsProvider, useProjectLabelFilter } from "@/shared/labels";
import { BoardTaskDeleteDialog } from "./BoardTaskDeleteDialog";
import { useBoardTaskDeletions } from "./BoardTaskDeletion";
import { WorkflowValidationIssues } from "@/shared/workflow-validation";
import { ErrorState, FloatingNoticeIsland, LoadingState } from "@/ui";
import { BoardHorizontalScrollbar } from "./BoardHorizontalScrollbar";
import { BoardRailMotionController } from "./BoardRailMotionController";
import { taskDeleteWindowOptions, type TaskDeleteTarget } from "./taskDeleteConfirmationModel";
import type { BoardColumnDropState } from "./BoardDragTypes";
import { classifyBoardColumnDropState, useBoardDragLifecycle } from "./BoardDragState";
import { BoardBackgroundRefreshNotice } from "./BoardBackgroundRefreshNotice";
import { BoardNoWorkflowState } from "./BoardNoWorkflowState";
import { classifyDrop } from "./BoardDropActions";
import { ManualMoveDialog } from "./ManualMoveDialog";
import { useBoardInitiatingActionController } from "./useBoardInitiatingActionController";
import "./board.css";
import { BoardFilterRow } from "./BoardFilterRow";
import { BoardQueryProvider } from "./BoardQueryContext";
import { useBoardNavigation, useCloseBoardTask } from "./BoardNavigation";
import { useBoard, useBoardTaskActions, useProjectBoardSubscription } from "./useBoardData";
import { useBoardLoadErrorReporter } from "./useBoardLoadErrorReporter";

export type BoardRouteProps = Readonly<{
  projectId: string;
  workflowId: string | undefined;
  selectedTaskId: string;
}>;

const emptyExpandedEmptyColumnIDs: ReadonlySet<string> = new Set();

const manualMoveBlockerTranslationKeys = {
  invalid_workflow: "board.moveBlockedInvalidWorkflow",
  no_source_position: "board.moveBlockedNoSource",
  unsupported_destination: "board.moveBlockedUnsupportedDestination",
  lifecycle_conflict: "board.moveBlockedLifecycle",
  context_session_unavailable: "board.moveBlockedContextSession",
  no_usable_transition: "board.moveBlockedNoUsableTransition",
  parallel_branch_requires_fan_out: "board.moveBlockedFanOut",
} as const;

function manualMoveBlockerCopy(reason: string, translate: (key: string) => string): string {
  const key =
    Object.entries(manualMoveBlockerTranslationKeys).find(([candidate]) => candidate === reason)?.[1] ??
    "board.moveBlockedGeneric";
  return translate(key);
}

export function BoardRoute({ projectId, workflowId, selectedTaskId }: BoardRouteProps) {
  const reportBoardLoadError = useBoardLoadErrorReporter();
  return (
    <SidebarRootOwner>
      <ProjectLabelsProvider
        onBackgroundError={reportBoardLoadError}
        projectID={projectId}
        subscribeToProject={false}
      >
        <BoardRouteWithLabels
          onBackgroundError={reportBoardLoadError}
          projectId={projectId}
          selectedTaskId={selectedTaskId}
          workflowId={workflowId}
        />
      </ProjectLabelsProvider>
    </SidebarRootOwner>
  );
}

function BoardRouteWithLabels({
  onBackgroundError,
  projectId,
  workflowId,
  selectedTaskId,
}: BoardRouteProps &
  Readonly<{
    onBackgroundError(error: unknown): void;
  }>) {
  const filter = useProjectLabelFilter();
  return (
    <BoardQueryProvider
      key={`${projectId}:${workflowId ?? "default"}`}
      labelFilter={filter.state.filter}
      queriesEnabled={filter.persistence.status !== "loading"}
    >
      <BoardRouteData
        onBackgroundError={onBackgroundError}
        projectId={projectId}
        selectedTaskId={selectedTaskId}
        workflowId={workflowId}
      />
    </BoardQueryProvider>
  );
}

function BoardRouteData({
  onBackgroundError: reportBoardLoadError,
  projectId,
  workflowId,
  selectedTaskId,
}: BoardRouteProps &
  Readonly<{
    onBackgroundError(error: unknown): void;
  }>) {
  const { t } = useTranslation();
  const { push } = useStatusController();
  const reportBoardNavigationError = useCallback(
    (error: unknown) => {
      push({
        id: "board-navigation-error",
        tone: "danger",
        title: t("board.navigationFailed"),
        body: errorMessage(error),
        durationMs: Infinity,
      });
    },
    [push, t],
  );
  const boardQuery = useBoard(projectId, workflowId);
  const board = boardQuery.data;
  const selectedWorkflowID = board?.selectedWorkflow?.id;
  const { close: handleSelectedTaskDeleted } = useCloseBoardTask(
    projectId,
    workflowId,
    reportBoardNavigationError,
  );
  const observation = useProjectBoardSubscription(projectId, workflowId, {
    onBackgroundError: reportBoardLoadError,
    onSelectedTaskDeleted: handleSelectedTaskDeleted,
    selectedTaskID: selectedTaskId,
    selectedWorkflowID,
  });

  if (boardQuery.isPending && board === undefined) {
    return <LoadingState chromePadding reveal={false} title={t("states.loading")} />;
  }
  if (boardQuery.isError && board === undefined) {
    return (
      <ErrorState
        body={errorMessage(boardQuery.error)}
        chromePadding
        onRetry={boardQuery.refetch}
        reveal={false}
        retryLabel={t("app.retry")}
        title={t("states.error")}
      />
    );
  }
  if (board === undefined || !hasSelectedWorkflow(board)) {
    return (
      <>
        <BoardNoWorkflowState projectID={projectId} />
        {observation.error === null ? null : (
          <BoardBackgroundRefreshNotice error={observation.error} onRetry={observation.retry} />
        )}
      </>
    );
  }

  return (
    <>
      <BoardContent
        board={board}
        boardQueryWorkflowID={workflowId}
        boardRefreshError={boardQuery.isError ? boardQuery.error : null}
        onBoardRefreshRetry={() => {
          boardQuery.refetch();
        }}
        selectedTaskId={selectedTaskId}
      />
      {observation.error === null ? null : (
        <BoardBackgroundRefreshNotice error={observation.error} onRetry={observation.retry} />
      )}
    </>
  );
}

function BoardContent({
  board,
  boardQueryWorkflowID,
  boardRefreshError,
  onBoardRefreshRetry,
  selectedTaskId,
}: Readonly<{
  board: SelectedWorkflowBoard;
  boardQueryWorkflowID: string | undefined;
  boardRefreshError: Error | null;
  onBoardRefreshRetry(): void;
  selectedTaskId: string;
}>) {
  const { t } = useTranslation();
  const [expandedEmptyColumns, setExpandedEmptyColumns] = useState<
    Readonly<{ ids: ReadonlySet<string>; scope: string }>
  >(() => ({ ids: new Set(), scope: "" }));
  const { push } = useStatusController();
  const { api, nativeBridge, logger } = useAppServices();
  const scrollportRef = useRef<HTMLDivElement | null>(null);
  const actions = useBoardTaskActions(board.projectID);
  const deletions = useBoardTaskDeletions();
  const reportActionError = useCallback(
    (id: string, title: string, error: unknown) => {
      reportNonCancelledError(error, (failure) => {
        const body = errorMessage(failure);
        push({ id, tone: "danger", title, body, durationMs: Infinity });
      });
    },
    [push],
  );
  const reportMoveError = useCallback(
    (error: unknown) => {
      reportActionError("board-move-error", t("board.moveFailed"), error);
    },
    [reportActionError, t],
  );
  const reportMovePreviewBlocked = useCallback(
    (reason: string) => {
      push({
        id: "board-move-preview-blocked",
        tone: "warning",
        title: t("board.moveBlocked"),
        body: manualMoveBlockerCopy(reason, t),
        durationMs: Infinity,
      });
    },
    [push, t],
  );
  const { initiatingAction, runCardAction } = useBoardInitiatingActionController({
    api,
    moveErrorTitle: t("board.moveFailed"),
    onActionError: reportActionError,
    onApplied: actions.refresh,
    refreshErrorTitle: t("board.loadFailed"),
    startErrorTitle: t("board.startFailed"),
    resumeErrorTitle: t("board.resumeFailed"),
  });
  const manualMove = initiatingAction.pending?.kind === "move_preview" ? initiatingAction.pending : null;
  const dragDisabled = !board.selectedWorkflow.validForTaskCreation;
  const {
    activeDrag,
    pendingCardMove,
    autoScroll: dragAutoScroll,
    cancel: cancelActiveDrag,
    drop: settleCardDrop,
    dragBlocked,
    start: startActiveDrag,
  } = useBoardDragLifecycle({
    disabled: dragDisabled,
    rootRef: scrollportRef,
    pendingTaskIDs: initiatingAction.pendingStartMoveTaskIDs,
    confirmationTaskID: initiatingAction.confirmationTaskID,
  });
  const reportNavigationError = useCallback(
    (error: unknown) => {
      reportActionError("board-navigation-error", t("board.navigationFailed"), error);
    },
    [reportActionError, t],
  );
  const taskDeleteDialog = useNativeDialogFallback<TaskDeleteTarget>({
    errorNoticeID: "task-delete-window-error",
    errorTitle: t("board.deleteTaskWindowError"),
    nativeAvailable: nativeBridge.capabilities.dialogWindows,
    openNative: async (target) => {
      await nativeBridge.dialogs.openWindow(taskDeleteWindowOptions(target, t("board.deleteTaskTitle")));
    },
    renderFallback: (target, close) => (
      <BoardTaskDeleteDialog
        key={target.taskID}
        taskID={target.taskID}
        deletions={deletions}
        projectID={board.projectID}
        workflowID={board.selectedWorkflow.id}
        selectedTaskID={selectedTaskId}
        onClose={close}
        onError={reportDeleteError}
        onNavigationError={reportNavigationError}
      />
    ),
  });

  const activeColumns = useMemo(
    () => board.columns.filter((column) => !column.isBacklog && !column.isDone),
    [board.columns],
  );
  const firstActive = activeColumns[0];
  const columnExpansionScope = `${board.projectID}:${board.selectedWorkflow.id}`;
  const expandedEmptyColumnIDs =
    expandedEmptyColumns.scope === columnExpansionScope
      ? expandedEmptyColumns.ids
      : emptyExpandedEmptyColumnIDs;
  useWindowChromeTitle(board.selectedWorkflow.name || board.projectName);
  const {
    openTask,
    openDependencies: openTaskDependencies,
    selectWorkflow,
    openTasks: openProjectTasks,
    openNewTask,
    openLinkWorkflow,
  } = useBoardNavigation(board.projectID, board.selectedWorkflow.id, {
    selectedTaskID: selectedTaskId,
    boardQueryWorkflowID,
    report: reportNavigationError,
  });

  function dropTask(targetID: string | number | null, rect: DOMRectReadOnly): void {
    const column = board.columns.find((item) => item.id === targetID);
    const dragPayload = activeDrag === null ? null : activeDrag.payload;
    if (column === undefined) {
      cancelActiveDrag();
      return;
    }
    if (dragPayload === null) {
      cancelActiveDrag();
      reportRejectedDrop();
      return;
    }
    const dropAction = classifyDrop(column, dragPayload, firstActive?.id);
    if (dropAction.kind === "start") {
      settleCardDrop(column.id, rect);
      runCardAction(startTaskInitiatingAction(dragPayload.taskID));
      return;
    }
    if (dropAction.kind === "move") {
      settleCardDrop(column.id, rect);
      initiatingAction.preview({
        taskID: dragPayload.taskID,
        targetNodeID: column.id,
        execute: async () => api.previewMoveTask(dragPayload.taskID, column.id),
        onBlocked: reportMovePreviewBlocked,
        onError: reportMoveError,
      });
      return;
    }
    cancelActiveDrag();
    reportRejectedDrop();
  }

  function interruptTask(taskID: string): void {
    actions.interrupt.execute(taskID, reportInterruptError);
  }

  function resumeTask(taskID: string): void {
    runCardAction(resumeTaskInitiatingAction(taskID));
  }

  function deleteTask(taskID: string): void {
    void taskDeleteDialog.open({ taskID });
  }

  function reportInterruptError(error: unknown): void {
    reportActionError("board-interrupt-error", t("board.interruptFailed"), error);
  }

  function reportDeleteError(error: unknown): void {
    reportActionError("board-delete-error", t("board.deleteFailed"), error);
  }

  function reportRejectedDrop(): void {
    push({
      id: "board-drop-rejected",
      tone: "warning",
      title: t("board.dropRejected"),
      body: t("board.dropRejectedBody"),
    });
  }

  function columnDropState(column: BoardColumn): BoardColumnDropState {
    return classifyBoardColumnDropState({
      column,
      drag: activeDrag,
      dragBlocked,
      firstActiveID: firstActive?.id,
    });
  }

  function columnIsCollapsed(column: BoardColumn): boolean {
    return (
      !column.isBacklog &&
      column.id !== firstActive?.id &&
      column.taskCount === 0 &&
      !expandedEmptyColumnIDs.has(column.id)
    );
  }

  function expandColumn(columnID: string): void {
    setExpandedEmptyColumns((current) => {
      const next = new Set(current.scope === columnExpansionScope ? current.ids : []);
      next.add(columnID);
      return { ids: next, scope: columnExpansionScope };
    });
  }

  function handleTaskInitiatingDialogResult(result: TaskInitiatingActionDialogResult): void {
    if (result.kind === "view_dependencies") {
      openTaskDependencies(result.taskID);
      return;
    }
    runCardAction(result.action, result.selection);
  }

  return (
    <div className="relative flex h-full min-h-0 min-w-0 w-full flex-col">
      <div className="flex shrink-0 items-center gap-[var(--space-2)] px-[var(--space-2)] pt-[var(--space-2)]">
        <BoardFilterRow
          activeWorkflow={board.selectedWorkflow}
          onLinkWorkflow={openLinkWorkflow}
          onNewTask={openNewTask}
          onOpenTask={openTask}
          onOpenTasks={openProjectTasks}
          onSelectWorkflow={selectWorkflow}
          projectID={board.projectID}
          workflows={board.workflows}
        />
      </div>
      <div className="relative min-h-0 min-w-0 flex-1">
        <DragDropSurface
          onCancel={(cause) => {
            cancelActiveDrag();
            if (cause) {
              void logger.append("warn", "Board drag cancelled without a measured position.", {
                error: errorMessage(cause),
              });
            }
          }}
          onDrop={dropTask}
          dropAnimation={pendingCardMove === null ? undefined : null}
        >
          <div
            className="h-full min-h-0 min-w-0 w-full overflow-x-auto hide-scrollbar"
            data-testid="board-scrollport"
            ref={scrollportRef}
            role="list"
          >
            <BoardRailMotionController
              activeDrag={activeDrag}
              board={board}
              columnDropState={columnDropState}
              columnIsCollapsed={columnIsCollapsed}
              dragDisabled={dragDisabled}
              firstActiveID={firstActive?.id}
              onCardClick={openTask}
              onCardDragStart={startActiveDrag}
              onDeleteTask={deleteTask}
              onExpandColumn={expandColumn}
              onInterruptTask={interruptTask}
              onRegisterColumnScrollport={dragAutoScroll.registerColumnScrollport}
              pendingCardMove={pendingCardMove}
              onResumeTask={resumeTask}
              pendingInterruptTaskIDs={actions.interrupt.pendingTaskIDs}
              pendingResumeTaskIDs={initiatingAction.pendingResumeTaskIDs}
              pendingStartMoveTaskIDs={initiatingAction.pendingStartMoveTaskIDs}
              scrollportRef={scrollportRef}
            />
          </div>
        </DragDropSurface>
        <BoardHorizontalScrollbar scrollportRef={scrollportRef} />
      </div>
      {manualMove === null ? null : (
        <ManualMoveDialog
          key={manualMove.action.actionID.toJSONValue()}
          onCancel={initiatingAction.close}
          onSubmit={(input) => {
            initiatingAction.close();
            runCardAction(
              moveTaskInitiatingAction(
                {
                  ...manualMove.action.input,
                  ...(input.transitionKey === undefined ? {} : { transitionKey: input.transitionKey }),
                  ...(input.values === undefined ? {} : { values: input.values }),
                },
                manualMove.action.actionID,
              ),
            );
          }}
          preview={manualMove.preview}
        />
      )}
      <TaskInitiatingActionDialogs
        continuation={initiatingAction}
        onResult={handleTaskInitiatingDialogResult}
      />
      {taskDeleteDialog.fallback}
      {boardRefreshError === null ? null : (
        <BoardBackgroundRefreshNotice error={boardRefreshError} onRetry={onBoardRefreshRetry} />
      )}
      <BoardWorkflowIssuesNotice workflow={board.selectedWorkflow} />
    </div>
  );
}

function BoardWorkflowIssuesNotice({
  workflow,
}: Readonly<{ workflow: SelectedWorkflowBoard["selectedWorkflow"] }>) {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(false);
  if (workflow.validForTaskCreation) {
    return null;
  }
  return (
    <FloatingNoticeIsland
      collapsed={collapsed}
      collapseLabel={t("app.collapse")}
      expandLabel={t("app.expand")}
      onCollapsedChange={setCollapsed}
      positionClassName="right-[var(--space-4)] bottom-[var(--space-4)]"
      title={t("board.workflowIssues")}
      tone="danger"
    >
      <WorkflowValidationIssues errors={workflow.validationErrors} />
    </FloatingNoticeIsland>
  );
}
