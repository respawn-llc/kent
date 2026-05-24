/* eslint-disable max-lines -- Board route coordinates data, drag/drop, and board-level dialogs. */
import type { DragEvent, SyntheticEvent } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { BoardColumn, WorkflowBoard } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppNavigation } from "../../app/navigation";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useSidebar } from "../../app/sidebarContext";
import { useStatusController } from "../../app/useStatusController";
import { useWindowChromeTitle } from "../../app/windowChromeTitle";
import { Button, EmptyState, ErrorState, FloatingNoticeIsland } from "../../ui";
import { WorkflowValidationIssues } from "../workflow/WorkflowValidationIssues";
import { BoardColumnController } from "./BoardColumnController";
import { BoardHoverMenu } from "./BoardHoverMenu";
import { KanbanGroup } from "./BoardColumns";
import { toKanbanGroupVM } from "./BoardColumnViewModel";
import {
  type BoardCardDragPayload,
  type BoardColumnDropState,
  boardCardDragPayloadType,
  decodeBoardCardDragPayload,
} from "./BoardDragTypes";
import {
  classifyDrop,
  missingInputValues,
  type PendingDrop,
  type PendingMissingInputDrop,
} from "./BoardDropActions";
import { MissingInputsDialog, RollbackStartDialog } from "./BoardDropDialogs";
import { boardSections } from "./BoardModel";
import "./board.css";
import { useBoard, useBoardTaskActions, useProjectBoardSubscription } from "./useBoardData";

export type BoardRouteProps = Readonly<{
  projectId: string;
  workflowId: string;
  selectedTaskId: string;
  resumeRunId: string;
}>;

export function BoardRoute({ projectId, workflowId, selectedTaskId, resumeRunId }: BoardRouteProps) {
  const { t } = useTranslation();
  const { push } = useStatusController();
  const reportBoardLoadError = useCallback(
    (error: unknown) => {
      push({
        id: "board-load-error",
        tone: "danger",
        title: t("board.loadFailed"),
        body: errorMessage(error),
        dismissible: false,
      });
    },
    [push, t],
  );
  const boardQuery = useBoard(projectId, workflowId);
  const board = boardQuery.data;
  useProjectBoardSubscription(projectId, workflowId, {
    onBackgroundError: reportBoardLoadError,
    selectedWorkflowID: board?.selectedWorkflow.id ?? workflowId,
  });

  if (boardQuery.isPending) {
    return <p>{t("states.loading")}</p>;
  }
  if (boardQuery.isError) {
    return (
      <ErrorState
        body={errorMessage(boardQuery.error)}
        chromePadding
        onRetry={() => void boardQuery.refetch().catch(reportBoardLoadError)}
        reveal={false}
        retryLabel={t("app.retry")}
        title={t("states.error")}
      />
    );
  }
  if (board === undefined || board.workflows.length === 0) {
    return <BoardNoWorkflowState projectID={projectId} />;
  }

  return (
    <BoardContent
      board={board}
      boardQueryWorkflowID={workflowId}
      resumeRunId={resumeRunId}
      selectedTaskId={selectedTaskId}
    />
  );
}

function BoardContent({
  board,
  boardQueryWorkflowID,
  selectedTaskId,
  resumeRunId,
}: Readonly<{
  board: WorkflowBoard;
  boardQueryWorkflowID: string;
  selectedTaskId: string;
  resumeRunId: string;
}>) {
  const { t } = useTranslation();
  const [workflowIssuesCollapsed, setWorkflowIssuesCollapsed] = useState(false);
  const [activeDrag, setActiveDrag] = useState<BoardCardDragPayload | null>(null);
  const [rollbackDrop, setRollbackDrop] = useState<PendingDrop | null>(null);
  const [missingInputDrop, setMissingInputDrop] = useState<PendingMissingInputDrop | null>(null);
  const activeDragRef = useRef<BoardCardDragPayload | null>(null);
  const { push } = useStatusController();
  const navigation = useAppNavigation();
  const scrollportRef = useRef<HTMLDivElement | null>(null);
  const { openSidebar } = useSidebar();
  const connection = useConnectionSnapshot();
  const actions = useBoardTaskActions(board.projectID, boardQueryWorkflowID, board.selectedWorkflow.id);
  const actionsDisabled = connection.phase !== "connected";

  const activeColumns = useMemo(
    () => board.columns.filter((column) => !column.isBacklog && !column.isDone),
    [board.columns],
  );
  const sections = useMemo(() => boardSections(board), [board]);
  const firstActive = activeColumns[0];
  useWindowChromeTitle(board.selectedWorkflow.name || board.projectName);
  const reportActionError = useCallback(
    (id: string, title: string, error: unknown) => {
      const body = errorMessage(error);
      push({ id, tone: "danger", title, body, dismissible: false });
    },
    [push],
  );
  const reportCardsLoadError = useCallback(
    (error: unknown) => {
      reportActionError("board-cards-load-error", t("board.cardsLoadFailed"), error);
    },
    [reportActionError, t],
  );
  const reportNavigationError = useCallback(
    (error: unknown) => {
      reportActionError("board-navigation-error", t("board.navigationFailed"), error);
    },
    [reportActionError, t],
  );

  useEffect(() => {
    if (selectedTaskId.length === 0) {
      return;
    }
    let active = true;
    void openSidebar({
      kind: "taskDetail",
      mode: "overlay",
      onMutated: undefined,
      resumeRunID: resumeRunId,
      taskID: selectedTaskId,
    }).then((result) => {
      if (active && result.status === "canceled" && result.reason === "closed") {
        void navigation.closeProjectTask(board.projectID, board.selectedWorkflow.id).catch(reportNavigationError);
      }
    });
    return () => {
      active = false;
    };
  }, [
    board.projectID,
    board.selectedWorkflow.id,
    navigation,
    openSidebar,
    reportNavigationError,
    resumeRunId,
    selectedTaskId,
  ]);

  function dropTask(event: DragEvent<HTMLElement>, column: BoardColumn): void {
    event.preventDefault();
    const dragPayload = activeDragRef.current ?? dragPayloadFromDataTransfer(event.dataTransfer);
    activeDragRef.current = null;
    setActiveDrag(null);
    if (
      dragPayload === null ||
      connection.phase !== "connected" ||
      !board.selectedWorkflow.validForTaskCreation
    ) {
      reportRejectedDrop();
      return;
    }
    const dropAction = classifyDrop(column, dragPayload, firstActive?.id);
    if (dropAction.kind === "start") {
      void actions.start.mutateAsync(dragPayload.taskID).catch(reportStartError);
      return;
    }
    if (dropAction.kind === "move") {
      const moveInput = {
        taskID: dragPayload.taskID,
        targetNodeID: column.id,
        ...(dropAction.allowMissingEdge === undefined
          ? {}
          : { allowMissingEdge: dropAction.allowMissingEdge }),
        ...(dropAction.autoApprove === undefined ? {} : { autoApprove: dropAction.autoApprove }),
      };
      void actions.move.mutateAsync(moveInput).catch(reportMoveError);
      return;
    }
    if (dropAction.kind === "confirmRollback") {
      setRollbackDrop({ taskID: dragPayload.taskID, targetColumn: column });
      return;
    }
    if (dropAction.kind === "reject") {
      reportRejectedDrop();
      return;
    }
    setMissingInputDrop({
      taskID: dragPayload.taskID,
      targetColumn: column,
      fields: column.transitionOutputFields,
      values: missingInputValues(column.transitionOutputFields),
    });
  }

  function interruptTask(taskID: string, runID: string): void {
    void actions.interrupt.mutateAsync({ taskID, runID }).catch(reportInterruptError);
  }

  function resumeTask(taskID: string, runID: string): void {
    void actions.resume.mutateAsync({ taskID, runID }).catch(reportResumeError);
  }

  function reportStartError(error: unknown): void {
    reportActionError("board-start-error", t("board.startFailed"), error);
  }

  function reportMoveError(error: unknown): void {
    reportActionError("board-move-error", t("board.moveFailed"), error);
  }

  function reportInterruptError(error: unknown): void {
    reportActionError("board-interrupt-error", t("board.interruptFailed"), error);
  }

  function reportResumeError(error: unknown): void {
    reportActionError("board-resume-error", t("board.resumeFailed"), error);
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
    if (activeDrag === null) {
      return "idle";
    }
    if (actionsDisabled || !board.selectedWorkflow.validForTaskCreation) {
      return "blocked";
    }
    const manualTargets = new Set(activeDrag.manualMoveTargetNodeIDs);
    const canStartHere = activeDrag.canStart && column.id === firstActive?.id;
    return canStartHere || manualTargets.has(column.id) ? "allowed" : "blocked";
  }

  function confirmRollbackDrop(): void {
    if (rollbackDrop === null) {
      return;
    }
    const drop = rollbackDrop;
    setRollbackDrop(null);
    void actions.move
      .mutateAsync({ taskID: drop.taskID, targetNodeID: drop.targetColumn.id, autoApprove: true })
      .catch(reportMoveError);
  }

  function submitMissingInputDrop(event: SyntheticEvent<HTMLFormElement>): void {
    event.preventDefault();
    if (missingInputDrop === null) {
      return;
    }
    const drop = missingInputDrop;
    setMissingInputDrop(null);
    void actions.move
      .mutateAsync({
        taskID: drop.taskID,
        targetNodeID: drop.targetColumn.id,
        outputValues: drop.values,
        allowMissingEdge: true,
        autoApprove: drop.targetColumn.kind === "agent",
      })
      .catch(reportMoveError);
  }

  function openTask(taskID: string): void {
    void navigation.openProjectTask(board.projectID, board.selectedWorkflow.id, taskID).catch(reportNavigationError);
  }

  function selectWorkflow(workflowID: string): void {
    void navigation.openProject(board.projectID, workflowID).catch(reportNavigationError);
  }

  function editWorkflow(workflowID: string): void {
    void navigation.openWorkflowEditor({ projectID: board.projectID, workflowID }).catch(reportNavigationError);
  }

  function openNewTask(): void {
    void openSidebar({
      boardQueryWorkflowID,
      kind: "newTask",
      mode: "overlay",
      projectID: board.projectID,
      workflowID: board.selectedWorkflow.id,
    });
  }

  function openLinkWorkflow(): void {
    void openSidebar({
      kind: "linkWorkflow",
      mode: "overlay",
      projectID: board.projectID,
      selectedWorkflowID: board.selectedWorkflow.id,
    });
  }

  return (
    <div className="relative h-full min-h-0 min-w-0 w-full">
      <div className="h-full min-h-0 min-w-0 w-full overflow-x-auto" ref={scrollportRef} role="list">
        <div
          className="flex h-full min-h-0 w-max min-w-full gap-[var(--space-2)] px-[var(--space-2)] pb-[var(--space-2)]"
          data-testid="board-column-rail"
        >
          {sections.map((section) =>
            section.kind === "group" ? (
              <KanbanGroup group={toKanbanGroupVM(section.group)} key={section.id}>
                {section.columns.map((column) => (
                  <BoardColumnController
                    actionsDisabled={actionsDisabled}
                    board={board}
                    column={column}
                    dropState={columnDropState(column)}
                    isFirstActive={column.id === firstActive?.id}
                    key={column.id}
                    onCardClick={openTask}
                    onCardDragEnd={() => {
                      activeDragRef.current = null;
                      setActiveDrag(null);
                    }}
                    onCardDragStart={(payload) => {
                      activeDragRef.current = payload;
                      setActiveDrag(payload);
                    }}
                    onCardsLoadError={reportCardsLoadError}
                    onDropTask={dropTask}
                    onInterruptTask={interruptTask}
                    onResumeTask={resumeTask}
                    scrollportRef={scrollportRef}
                  />
                ))}
              </KanbanGroup>
            ) : (
              <BoardColumnController
                actionsDisabled={actionsDisabled}
                board={board}
                column={section.column}
                dropState={columnDropState(section.column)}
                isFirstActive={section.column.id === firstActive?.id}
                key={section.id}
                onCardClick={openTask}
                onCardDragEnd={() => {
                  activeDragRef.current = null;
                  setActiveDrag(null);
                }}
                onCardDragStart={(payload) => {
                  activeDragRef.current = payload;
                  setActiveDrag(payload);
                }}
                onCardsLoadError={reportCardsLoadError}
                onDropTask={dropTask}
                onInterruptTask={interruptTask}
                onResumeTask={resumeTask}
                scrollportRef={scrollportRef}
              />
            ),
          )}
        </div>
      </div>
      <RollbackStartDialog
        onClose={() => {
          setRollbackDrop(null);
        }}
        onConfirm={confirmRollbackDrop}
        open={rollbackDrop !== null}
      />
      <MissingInputsDialog
        drop={missingInputDrop}
        onClose={() => {
          setMissingInputDrop(null);
        }}
        onSubmit={submitMissingInputDrop}
        onValueChange={(fieldName, value) => {
          setMissingInputDrop((current) =>
            current === null ? null : { ...current, values: { ...current.values, [fieldName]: value } },
          );
        }}
      />
      {!board.selectedWorkflow.validForTaskCreation ? (
        <FloatingNoticeIsland
          collapsed={workflowIssuesCollapsed}
          collapseLabel={t("app.collapse")}
          expandLabel={t("app.expand")}
          onCollapsedChange={setWorkflowIssuesCollapsed}
          positionClassName="right-[var(--space-4)] bottom-[var(--space-4)]"
          title={t("board.workflowIssues")}
          tone="danger"
        >
          <WorkflowValidationIssues errors={board.selectedWorkflow.validationErrors} />
        </FloatingNoticeIsland>
      ) : null}
      <BoardHoverMenu
        board={board}
        canCreateTask={connection.phase === "connected"}
        onNewTask={openNewTask}
        onWorkflowEdit={editWorkflow}
        onWorkflowLink={openLinkWorkflow}
        onWorkflowSelect={selectWorkflow}
      />
    </div>
  );
}

function BoardNoWorkflowState({ projectID }: Readonly<{ projectID: string }>) {
  const { t } = useTranslation();
  const { openSidebar } = useSidebar();
  const connection = useConnectionSnapshot();
  const actionsDisabled = connection.phase !== "connected";
  return (
    <EmptyState
      actions={
        <>
          <Button
            disabled={actionsDisabled}
            onClick={() => {
              void openSidebar({ kind: "linkWorkflow", mode: "overlay", projectID });
            }}
          >
            {t("workflowLibrary.linkWorkflow")}
          </Button>
          <Button
            disabled={actionsDisabled}
            onClick={() => {
              void openSidebar({ kind: "workflowCreate", mode: "overlay", projectID });
            }}
            variant="primary"
          >
            {t("workflowLibrary.createWorkflow")}
          </Button>
        </>
      }
      body={t("board.noWorkflowBody")}
      chromePadding
      title={t("board.noWorkflowTitle")}
    />
  );
}

function dragPayloadFromDataTransfer(dataTransfer: DataTransfer): BoardCardDragPayload | null {
  return decodeBoardCardDragPayload(dataTransfer.getData(boardCardDragPayloadType));
}
