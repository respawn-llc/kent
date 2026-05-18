import type { DragEvent } from "react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import type { BoardColumn, WorkflowBoard, WorkflowPickerItem } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppNavigation } from "../../app/navigation";
import { projectToBoardTransitionName } from "../../app/navigationTransitions";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useNativeDialogFallback } from "../../app/useNativeDialogFallback";
import { useWindowChromeTitle } from "../../app/windowChromeTitle";
import { EmptyState, ErrorState, FloatingNoticeIsland } from "../../ui";
import { TaskDetailDialog } from "../task-detail/TaskDetailDialog";
import { useOpenTaskDetail } from "../task-detail/useOpenTaskDetail";
import { NewTaskFallbackDialog } from "../tasks/NewTaskDialog";
import { newTaskWindowOptions } from "../tasks/newTaskWindowOptions";
import { BoardHoverMenu } from "./BoardHoverMenu";
import { KanbanColumn, KanbanGroup } from "./BoardColumns";
import { boardSections, cardsForColumn } from "./BoardModel";
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
                onRetry={() => void boardQuery.refetch()}
                reveal={false}
                retryLabel={t("app.retry")}
                title={t("states.error")}
            />
        );
    }
    if (board === undefined || board.workflows.length === 0) {
        return <EmptyState body={t("board.noWorkflowBody")} title={t("board.noWorkflowTitle")} />;
    }

    return (
        <BoardContent
            board={board}
            hasMoreCards={boardQuery.hasNextPage}
            isLoadingMoreCards={boardQuery.isFetchingNextPage}
            onLoadMoreCards={() => void boardQuery.fetchNextPage()}
            resumeRunId={resumeRunId}
            selectedTaskId={selectedTaskId}
        />
    );
}

function BoardContent({
    board,
    hasMoreCards,
    isLoadingMoreCards,
    onLoadMoreCards,
    selectedTaskId,
    resumeRunId,
}: Readonly<{
    board: WorkflowBoard;
    hasMoreCards: boolean;
    isLoadingMoreCards: boolean;
    onLoadMoreCards: () => void;
    selectedTaskId: string;
    resumeRunId: string;
}>) {
    const { t } = useTranslation();
    const [doneExpanded, setDoneExpanded] = useState(false);
    const [workflowIssuesCollapsed, setWorkflowIssuesCollapsed] = useState(false);
    const navigation = useAppNavigation();
    const { nativeBridge } = useAppServices();
    const openTaskDetail = useOpenTaskDetail();
    const connection = useConnectionSnapshot();
    const actions = useBoardTaskActions(board.projectID, board.selectedWorkflow.id);
    const actionsDisabled = connection.phase !== "connected";
    const activeColumns = useMemo(
        () => board.columns.filter((column) => !column.isBacklog && !column.isDone),
        [board.columns],
    );
    const sections = useMemo(() => boardSections(board), [board]);
    const firstActive = activeColumns[0];
    const canToggleDone = board.hasHiddenDoneCards;
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
        const taskID = event.dataTransfer.getData("text/task-id");
        const card = board.cards.find((candidate) => candidate.id === taskID);
        if (taskID.length === 0 || connection.phase !== "connected" || !board.selectedWorkflow.validForTaskCreation) {
            return;
        }
        if (card?.actions.canStart === true && column.id === firstActive?.id) {
            void actions.start.mutateAsync(taskID);
            return;
        }
        if (card?.actions.manualMoveTargetNodeIDs.includes(column.id) === true) {
            void actions.move.mutateAsync({ taskID, targetNodeID: column.id });
        }
    }

    return (
        <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-[var(--space-4)]">
            <header
                className="flex flex-wrap items-start justify-between gap-[var(--space-4)]"
                data-testid="board-transition-source"
                style={{ viewTransitionName: projectToBoardTransitionName }}
            >
                {board.selectedWorkflow.validForTaskCreation ? (
                    <p className="m-0 text-[var(--color-muted)]">{t("board.dragStart")}</p>
                ) : null}
            </header>
            <div className="min-h-0 overflow-x-auto overflow-y-hidden pr-[var(--shadow-bleed-island)] pb-[var(--shadow-bleed-island)] hide-scrollbar -mr-[var(--shadow-bleed-island)] -mb-[var(--shadow-bleed-island)]" role="list">
                <div className="flex h-full min-h-0 gap-[var(--space-3)]">
                    {sections.map((section) =>
                        section.kind === "group" ? (
                            <KanbanGroup
                                board={board}
                                actionsDisabled={actionsDisabled}
                                canToggleDone={canToggleDone}
                                canRunTasks={board.selectedWorkflow.validForTaskCreation}
                                columns={section.columns}
                                doneExpanded={doneExpanded}
                                firstActiveColumnID={firstActive?.id ?? ""}
                                group={section.group}
                                hasMoreCards={hasMoreCards}
                                isLoadingMoreCards={isLoadingMoreCards}
                                key={section.id}
                                onCardClick={(taskID) => {
                                    openTaskDetail(taskID, "", () => {
                                        void navigation.openProjectTask(board.projectID, board.selectedWorkflow.id, taskID);
                                    });
                                }}
                                onDropTask={dropTask}
                                onInterruptTask={(taskID, runID) => void actions.interrupt.mutateAsync({ taskID, runID })}
                                onLoadMoreCards={onLoadMoreCards}
                                onResumeTask={(taskID, runID) => void actions.resume.mutateAsync({ taskID, runID })}
                                onToggleDone={() => {
                                    setDoneExpanded((current) => !current);
                                }}
                            />
                        ) : (
                            <KanbanColumn
                                cards={cardsForColumn(board, section.column, doneExpanded)}
                                actionsDisabled={actionsDisabled}
                                canToggleDone={canToggleDone}
                                canRunTasks={board.selectedWorkflow.validForTaskCreation}
                                column={section.column}
                                doneExpanded={doneExpanded}
                                hasMoreCards={hasMoreCards}
                                isLoadingMoreCards={isLoadingMoreCards}
                                isFirstActive={section.column.id === firstActive?.id}
                                key={section.id}
                                onCardClick={(taskID) => {
                                    openTaskDetail(taskID, "", () => {
                                        void navigation.openProjectTask(board.projectID, board.selectedWorkflow.id, taskID);
                                    });
                                }}
                                onDropTask={dropTask}
                                onInterruptTask={(taskID, runID) => void actions.interrupt.mutateAsync({ taskID, runID })}
                                onLoadMoreCards={onLoadMoreCards}
                                onResumeTask={(taskID, runID) => void actions.resume.mutateAsync({ taskID, runID })}
                                onToggleDone={() => {
                                    setDoneExpanded((current) => !current);
                                }}
                            />
                        ),
                    )}
                </div>
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
