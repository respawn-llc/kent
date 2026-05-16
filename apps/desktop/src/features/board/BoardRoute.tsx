import type { DragEvent } from "react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import type { BoardCard, BoardColumn, BoardGroup, WorkflowBoard, WorkflowPickerItem } from "../../api";
import { errorMessage } from "../../api/errors";
import { formatRelativeTime } from "../../app/formatters";
import { useAppNavigation } from "../../app/navigation";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { Badge, Button, EmptyState, ErrorState } from "../../ui";
import { TaskDetailDialog } from "../task-detail/TaskDetailDialog";
import { NewTaskDialog } from "../tasks/NewTaskDialog";
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
  useProjectBoardSubscription(projectId, board?.selectedWorkflow.id ?? workflowId, board?.latestEventSequence ?? 0);

  if (boardQuery.isPending) {
    return <p>{t("states.loading")}</p>;
  }
  if (boardQuery.isError) {
    return <ErrorState body={errorMessage(boardQuery.error)} onRetry={() => void boardQuery.refetch()} retryLabel={t("app.retry")} title={t("states.error")} />;
  }
  if (board === undefined || board.workflows.length === 0 || !board.selectedWorkflow.validForTaskCreation) {
    return <EmptyState body={t("board.noWorkflowBody")} title={t("board.noWorkflowTitle")} />;
  }

  return <BoardContent board={board} resumeRunId={resumeRunId} selectedTaskId={selectedTaskId} />;
}

function BoardContent({ board, selectedTaskId, resumeRunId }: Readonly<{ board: WorkflowBoard; selectedTaskId: string; resumeRunId: string }>) {
  const { t } = useTranslation();
  const [newTaskOpen, setNewTaskOpen] = useState(false);
  const [doneExpanded, setDoneExpanded] = useState(false);
  const navigation = useAppNavigation();
  const connection = useConnectionSnapshot();
  const actions = useBoardTaskActions(board.projectID, board.selectedWorkflow.id);
  const activeColumns = useMemo(() => board.columns.filter((column) => !column.isBacklog && !column.isDone), [board.columns]);
  const sections = useMemo(() => boardSections(board), [board]);
  const firstActive = activeColumns[0];

  function dropTask(event: DragEvent<HTMLElement>, column: BoardColumn): void {
    event.preventDefault();
    const taskID = event.dataTransfer.getData("text/task-id");
    if (taskID.length === 0 || connection.phase !== "connected") {
      return;
    }
    if (column.id === firstActive?.id) {
      void actions.start.mutateAsync(taskID);
      return;
    }
    if (column.isDone) {
      void actions.move.mutateAsync({ taskID, targetNodeID: column.id });
    }
  }

  return (
    <div className="board-page">
      <header className="board-header">
        <div>
          <p className="eyebrow">{board.projectKey}</p>
          <h1>{board.projectName}</h1>
          <p>{t("board.dragStart")}</p>
        </div>
        <div className="page-actions">
          <WorkflowPicker board={board} />
          <Button disabled={connection.phase !== "connected"} onClick={() => { setNewTaskOpen(true); }} variant="primary">
            {t("board.newTask")}
          </Button>
        </div>
      </header>
      <div className="kanban-board" role="list">
        {sections.map((section) => section.kind === "group" ? (
          <KanbanGroup
            board={board}
            columns={section.columns}
            doneExpanded={doneExpanded}
            firstActiveColumnID={firstActive?.id ?? ""}
            group={section.group}
            key={section.id}
            onCardClick={(taskID) => { navigation.openProjectTask(board.projectID, board.selectedWorkflow.id, taskID); }}
            onDropTask={dropTask}
            onToggleDone={() => { setDoneExpanded((current) => !current); }}
          />
        ) : (
          <KanbanColumn
            cards={cardsForColumn(board, section.column, doneExpanded)}
            column={section.column}
            doneExpanded={doneExpanded}
            isFirstActive={section.column.id === firstActive?.id}
            key={section.id}
            onCardClick={(taskID) => { navigation.openProjectTask(board.projectID, board.selectedWorkflow.id, taskID); }}
            onDropTask={dropTask}
            onToggleDone={() => { setDoneExpanded((current) => !current); }}
          />
        ))}
      </div>
      <NewTaskDialog board={board} onClose={() => { setNewTaskOpen(false); }} open={newTaskOpen} />
      <TaskDetailDialog
        onClose={() => { navigation.closeProjectTask(board.projectID, board.selectedWorkflow.id); }}
        open={selectedTaskId.length > 0}
        resumeRunId={resumeRunId}
        taskId={selectedTaskId}
      />
    </div>
  );
}

type BoardSection = Readonly<
  | { kind: "column"; id: string; column: BoardColumn }
  | { kind: "group"; id: string; group: BoardGroup; columns: readonly BoardColumn[] }
>;

function WorkflowPicker({ board }: Readonly<{ board: WorkflowBoard }>) {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  return (
    <details className="workflow-picker">
      <summary>
        {t("board.workflowPicker")}: {board.selectedWorkflow.name}
      </summary>
      <div className="workflow-picker__menu">
        {board.workflows.map((workflow) => (
          <WorkflowOption board={board} key={workflow.id} workflow={workflow} />
        ))}
      </div>
    </details>
  );

  function WorkflowOption({ workflow }: Readonly<{ board: WorkflowBoard; workflow: WorkflowPickerItem }>) {
    return (
      <button onClick={() => { navigation.openProject(board.projectID, workflow.id); }} type="button">
        <strong>{workflow.name}</strong>
        {workflow.isProjectDefault ? <Badge tone="info">{t("board.defaultWorkflow")}</Badge> : null}
      </button>
    );
  }
}

type KanbanColumnProps = Readonly<{
  column: BoardColumn;
  cards: readonly BoardCard[];
  doneExpanded: boolean;
  isFirstActive: boolean;
  onCardClick: (taskID: string) => void;
  onDropTask: (event: DragEvent<HTMLElement>, column: BoardColumn) => void;
  onToggleDone: () => void;
}>;

function KanbanGroup({ board, columns, doneExpanded, firstActiveColumnID, group, onCardClick, onDropTask, onToggleDone }: Readonly<{
  board: WorkflowBoard;
  columns: readonly BoardColumn[];
  doneExpanded: boolean;
  firstActiveColumnID: string;
  group: BoardGroup;
  onCardClick: (taskID: string) => void;
  onDropTask: (event: DragEvent<HTMLElement>, column: BoardColumn) => void;
  onToggleDone: () => void;
}>) {
  return (
    <section className="kanban-group" role="listitem">
      <header>
        <p className="eyebrow">{group.key}</p>
        <h2>{group.name}</h2>
      </header>
      <div className="kanban-group__columns">
        {columns.map((column) => (
          <KanbanColumn
            cards={cardsForColumn(board, column, doneExpanded)}
            column={column}
            doneExpanded={doneExpanded}
            isFirstActive={column.id === firstActiveColumnID}
            key={column.id}
            onCardClick={onCardClick}
            onDropTask={onDropTask}
            onToggleDone={onToggleDone}
          />
        ))}
      </div>
    </section>
  );
}

function KanbanColumn({ column, cards, doneExpanded, isFirstActive, onCardClick, onDropTask, onToggleDone }: KanbanColumnProps) {
  const { t } = useTranslation();
  return (
    <section aria-label={column.name} className="kanban-column" onDragOver={(event) => { event.preventDefault(); }} onDrop={(event) => { onDropTask(event, column); }} role="listitem">
      <header>
        <h2>{column.name}</h2>
        <Badge tone={isFirstActive ? "info" : "neutral"}>{t("board.taskCount", { count: column.taskCount })}</Badge>
      </header>
      {isFirstActive ? <p className="drop-hint">{t("board.dropToStart")}</p> : null}
      {column.isDone ? (
        <Button onClick={onToggleDone} variant="ghost">
          {doneExpanded ? t("board.collapseDone") : t("board.expandDone")}
        </Button>
      ) : null}
      <div className="kanban-column__cards">
        {cards.map((card) => (
          <TaskCard card={card} key={card.id} onClick={() => { onCardClick(card.id); }} />
        ))}
      </div>
    </section>
  );
}

function TaskCard({ card, onClick }: Readonly<{ card: BoardCard; onClick: () => void }>) {
  const { t } = useTranslation();
  return (
    <article aria-label={card.title} className="task-card" draggable={card.actions.canStart} onDragStart={(event) => { event.dataTransfer.setData("text/task-id", card.id); }}>
      <button onClick={onClick} type="button">
        <span className="mono">{card.shortID}</span>
        <strong>{card.title}</strong>
        <span>{card.bodyPreview}</span>
      </button>
      <div className="chip-row">
        <Badge tone="info">{card.status.label}</Badge>
        <Badge tone="neutral">{card.sourceWorkspace.name || t("board.workspace")}</Badge>
        <span>{formatRelativeTime(card.updatedAt)}</span>
      </div>
    </article>
  );
}

function cardsForColumn(board: WorkflowBoard, column: BoardColumn, doneExpanded: boolean): readonly BoardCard[] {
  if (column.isDone) {
    return doneExpanded ? board.cards.filter((card) => card.status.kind === "done") : board.donePreview;
  }
  if (column.isBacklog) {
    return board.cards.filter((card) => card.actions.canStart);
  }
  return board.cards.filter((card) => card.activeNodeIDs.includes(column.id));
}

function boardSections(board: WorkflowBoard): readonly BoardSection[] {
  const columnsByID = new Map(board.columns.map((column) => [column.id, column]));
  const consumed = new Set<string>();
  const backlog = board.columns.filter((column) => column.isBacklog);
  const done = board.columns.filter((column) => column.isDone);
  const groups = [...board.groups].sort((left, right) => left.sortOrder - right.sortOrder);
  const sections: BoardSection[] = [];

  for (const column of backlog) {
    consumed.add(column.id);
    sections.push({ kind: "column", id: column.id, column });
  }

  for (const group of groups) {
    const groupedColumns = group.nodeIDs
      .map((nodeID) => columnsByID.get(nodeID))
      .filter((column): column is BoardColumn => column !== undefined && !column.isBacklog && !column.isDone);
    const fallbackColumns = board.columns.filter((column) => column.groupID === group.id && !column.isBacklog && !column.isDone);
    const columns = groupedColumns.length > 0 ? groupedColumns : fallbackColumns;
    for (const column of columns) {
      consumed.add(column.id);
    }
    if (columns.length > 0) {
      sections.push({ kind: "group", id: group.id, group, columns });
    }
  }

  for (const column of board.columns) {
    if (!column.isBacklog && !column.isDone && !consumed.has(column.id)) {
      sections.push({ kind: "column", id: column.id, column });
    }
  }

  for (const column of done) {
    consumed.add(column.id);
    sections.push({ kind: "column", id: column.id, column });
  }

  return sections;
}
