import type { DragEvent } from "react";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { BoardColumn, WorkflowBoard, WorkflowPickerItem } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppNavigation } from "../../app/navigation";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useNativeDialogFallback } from "../../app/useNativeDialogFallback";
import { useStatusController } from "../../app/useStatusController";
import { useWindowChromeTitle } from "../../app/windowChromeTitle";
import { EmptyState, ErrorState, FloatingNoticeIsland } from "../../ui";
import { TaskDetailDialog } from "../task-detail/TaskDetailDialog";
import { useOpenTaskDetail } from "../task-detail/useOpenTaskDetail";
import { NewTaskFallbackDialog } from "../tasks/NewTaskDialog";
import { newTaskWindowOptions } from "../tasks/newTaskWindowOptions";
import { BoardColumnController } from "./BoardColumnController";
import { BoardHoverMenu } from "./BoardHoverMenu";
import { KanbanGroup } from "./BoardColumns";
import {
    type BoardCardDragPayload,
    type BoardColumnDropState,
    boardDragTypeCanStart,
    boardDragTypeManualTarget,
} from "./BoardDragTypes";
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
    const boardQuery = useBoard(projectId, workflowId);
    const board = boardQuery.data;
    useProjectBoardSubscription(
        projectId,
        workflowId,
        board?.selectedWorkflow.id ?? workflowId,
        board?.latestEventSequence ?? 0,
    );

    if (boardQuery.isPending) {
        return <p>{t("states.loading")}</p>;
    }
    if (boardQuery.isError) {
        return (
            <ErrorState
                body={errorMessage(boardQuery.error)}
                chromePadding
                onRetry={() => void boardQuery.refetch()}
                reveal={false}
                retryLabel={t("app.retry")}
                title={t("states.error")}
            />
        );
    }
    if (board === undefined || board.workflows.length === 0) {
        return <EmptyState body={t("board.noWorkflowBody")} chromePadding title={t("board.noWorkflowTitle")} />;
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
    const activeDragRef = useRef<BoardCardDragPayload | null>(null);
    const { push } = useStatusController();
    const navigation = useAppNavigation();
    const scrollportRef = useRef<HTMLDivElement | null>(null);
    const { nativeBridge } = useAppServices();
    const openTaskDetail = useOpenTaskDetail();
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
    const newTaskDialog = useNativeDialogFallback({
        errorNoticeID: "new-task-window-error",
        errorTitle: t("task.newWindowError"),
        openNative: async () => {
            await nativeBridge.dialogs.openWindow(
                newTaskWindowOptions({
                    projectID: board.projectID,
                    title: t("task.newTitle"),
                    workflowID: board.selectedWorkflow.id,
                }),
            );
        },
        renderFallback: (_payload, close) => (
            <NewTaskFallbackDialog
                onClose={close}
                projectID={board.projectID}
                workflowID={board.selectedWorkflow.id}
            />
        ),
    });

    function dropTask(event: DragEvent<HTMLElement>, column: BoardColumn): void {
        event.preventDefault();
        const dragPayload = activeDragRef.current ?? dragPayloadFromDataTransfer(event.dataTransfer);
        activeDragRef.current = null;
        setActiveDrag(null);
        if (dragPayload === null || connection.phase !== "connected" || !board.selectedWorkflow.validForTaskCreation) {
            reportRejectedDrop();
            return;
        }
        if (!dropAllowed(column, dragPayload, firstActive?.id)) {
            reportRejectedDrop();
            return;
        }
        if (dragPayload.canStart && column.id === firstActive?.id) {
            void actions.start.mutateAsync(dragPayload.taskID).catch(reportStartError);
            return;
        }
        if (dragPayload.manualMoveTargetNodeIDs.includes(column.id)) {
            void actions.move.mutateAsync({ taskID: dragPayload.taskID, targetNodeID: column.id }).catch(reportMoveError);
        }
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

    function reportActionError(id: string, title: string, error: unknown): void {
        push({ id, tone: "danger", title, body: errorMessage(error) });
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

    return (
        <div className="h-full min-h-0 min-w-0 w-full overflow-x-auto" ref={scrollportRef} role="list">
            <div
                className="flex h-full min-h-0 w-max min-w-full gap-[var(--space-2)] px-[var(--space-2)] pb-[var(--space-2)]"
                data-testid="board-column-rail"
            >
                {sections.map((section) =>
                    section.kind === "group" ? (
                        <KanbanGroup
                            group={section.group}
                            key={section.id}
                        >
                            {section.columns.map((column) => (
                                <BoardColumnController
                                    actionsDisabled={actionsDisabled}
                                    board={board}
                                    column={column}
                                    dropState={columnDropState(column)}
                                    isFirstActive={column.id === firstActive?.id}
                                    key={column.id}
                                    onCardClick={(taskID) => {
                                        openTaskDetail(taskID, "", () => {
                                            void navigation.openProjectTask(board.projectID, board.selectedWorkflow.id, taskID);
                                        });
                                    }}
                                    onDropTask={dropTask}
                                    onCardDragEnd={() => {
                                        activeDragRef.current = null;
                                        setActiveDrag(null);
                                    }}
                                    onCardDragStart={(payload) => {
                                        activeDragRef.current = payload;
                                        setActiveDrag(payload);
                                    }}
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
                            onCardClick={(taskID) => {
                                openTaskDetail(taskID, "", () => {
                                    void navigation.openProjectTask(board.projectID, board.selectedWorkflow.id, taskID);
                                });
                            }}
                            onDropTask={dropTask}
                            onCardDragEnd={() => {
                                activeDragRef.current = null;
                                setActiveDrag(null);
                            }}
                            onCardDragStart={(payload) => {
                                activeDragRef.current = payload;
                                setActiveDrag(payload);
                            }}
                            onInterruptTask={interruptTask}
                            onResumeTask={resumeTask}
                            scrollportRef={scrollportRef}
                        />
                    ),
                )}
            </div>
            <TaskDetailDialog
                onClose={() => {
                    void navigation.closeProjectTask(board.projectID, board.selectedWorkflow.id);
                }}
                open={selectedTaskId.length > 0}
                resumeRunId={resumeRunId}
                taskId={selectedTaskId}
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
                    <WorkflowValidationIssues workflow={board.selectedWorkflow} />
                </FloatingNoticeIsland>
            ) : null}
            <BoardHoverMenu
                board={board}
                canCreateTask={connection.phase === "connected"}
                onNewTask={() => {
                    void newTaskDialog.open(undefined);
                }}
            />
            {newTaskDialog.fallback}
        </div>
    );
}

function dropAllowed(
    column: BoardColumn,
    dragPayload: BoardCardDragPayload,
    firstActiveColumnID: string | undefined,
): boolean {
    if (dragPayload.canStart && column.id === firstActiveColumnID) {
        return true;
    }
    return dragPayload.manualMoveTargetNodeIDs.includes(column.id);
}

function dragPayloadFromDataTransfer(dataTransfer: DataTransfer): BoardCardDragPayload | null {
    const taskID = dataTransfer.getData("text/task-id");
    if (taskID.length === 0) {
        return null;
    }
    const types = Array.from(dataTransfer.types);
    return {
        taskID,
        canStart: types.includes(boardDragTypeCanStart),
        manualMoveTargetNodeIDs: types
            .map((type) => manualTargetNodeIDFromDragType(type))
            .filter((nodeID): nodeID is string => nodeID !== null),
    };
}

function manualTargetNodeIDFromDragType(type: string): string | null {
    const prefix = boardDragTypeManualTarget("");
    if (!type.startsWith(prefix)) {
        return null;
    }
    const nodeID = type.slice(prefix.length);
    return nodeID.length > 0 ? nodeID : null;
}

function WorkflowValidationIssues({ workflow }: Readonly<{ workflow: WorkflowPickerItem }>) {
    const { t } = useTranslation();
    const messages =
        workflow.validationErrors.length > 0
            ? workflow.validationErrors.map((issue) => issue.message)
            : [t("board.invalidWorkflowUnknown")];
    return (
        <ul className="workflow-issues-list m-0 grid max-w-[72ch] list-none gap-[var(--space-1)] p-0 text-sm leading-snug text-[var(--color-on-island)]">
            {messages.map((message) => (
                <li className="relative pl-[1.2rem]" key={message}>
                    <span>{message}</span>
                </li>
            ))}
        </ul>
    );
}
