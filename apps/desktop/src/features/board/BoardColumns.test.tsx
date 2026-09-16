import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DragDropSurface } from "@app/ui-kit";
import type { ReactElement } from "react";
import type * as ReactI18next from "react-i18next";

import { KanbanColumn } from "./BoardColumns";
import type { KanbanCardVM, KanbanColumnVM } from "./BoardColumnViewModel";
import { useBoardDragLifecycle, type ActiveBoardCardDrag } from "./BoardDragState";
import { projectPendingBoardCardMove } from "./BoardCardMotionModel";
import { useBoardInitiatingActionController } from "./useBoardInitiatingActionController";
import { TestAppProviders, createTestServices, startupRoutes } from "@/test-support/app-services";
import { startTaskInitiatingAction } from "@/shared/execution-target";
import type { TaskStartResponse } from "@/api";
import { deferred } from "@/test-support/chat-runtime";

vi.mock("react-i18next", async (importOriginal) => ({
  ...(await importOriginal<typeof ReactI18next>()),
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/app-facade", async (importOriginal) => ({
  ...(await importOriginal()),
  formatRelativeTime: () => "now",
  useOwnedSidebarRoots: () => ({ open: vi.fn() }),
}));

const column: KanbanColumnVM = {
  assigneeRole: "",
  id: "column-1",
  name: "Doing",
  taskCount: 1,
};

const card: KanbanCardVM = {
  actions: {
    canDelete: false,
    canInterrupt: false,
    canResume: true,
    canStart: false,
  },
  activeNodeIDs: ["column-1"],
  borderTone: "default",
  dependencyProgress: null,
  id: "task-1",
  labels: [],
  preview: { markdown: "", truncated: false },
  shortID: "KNT-1",
  statusKind: "interrupted",
  title: "Task",
  updatedAt: Date.UTC(2026, 0, 1),
  workspaceChipLabel: null,
};

const drag: ActiveBoardCardDrag = {
  instance: { columnID: column.id, taskID: card.id },
  lastCardIndex: 0,
  payload: {
    activeNodeIDs: card.activeNodeIDs,
    canStart: card.actions.canStart,
    statusKind: card.statusKind,
    taskID: card.id,
  },
  snapshot: card,
};

function renderColumn(column: ReactElement) {
  return render(
    <DragDropSurface onDrop={vi.fn()} onCancel={vi.fn()}>
      {column}
    </DragDropSurface>,
  );
}

describe("KanbanColumn retained replacement boundary", () => {
  beforeEach(() => {
    class TestPointerEvent extends MouseEvent {
      readonly isPrimary = true;
      readonly pointerId = 1;
      readonly pointerType = "mouse";
    }
    vi.stubGlobal("PointerEvent", TestPointerEvent);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("holds a dropped card at its destination while confirmation or execution is pending", async () => {
    const rootRef = { current: null };
    const { result, rerender } = renderHook(
      ({ actionPending }) =>
        useBoardDragLifecycle({
          disabled: false,
          rootRef,
          pendingTaskIDs: new Set(actionPending ? [card.id] : []),
          confirmationTaskID: null,
        }),
      { initialProps: { actionPending: false } },
    );
    const stored = new Map([
      [column.id, [card]],
      ["destination", []],
    ]);
    act(() => {
      result.current.start(drag);
    });
    rerender({ actionPending: true });
    act(() => {
      result.current.drop("destination", new DOMRect(350, 40, 200, 100));
    });
    expect(result.current.activeDrag).toBeNull();
    const pending = result.current.pendingCardMove;
    expect(pending).not.toBeNull();
    const optimistic = projectPendingBoardCardMove(stored, pending);
    expect(optimistic.get(column.id)).toEqual([]);
    expect(optimistic.get("destination")).toEqual([card]);
    expect(stored.get(column.id)).toEqual([card]);

    rerender({ actionPending: true });
    expect(result.current.pendingCardMove).toBe(pending);
    // Cancel or failure exposes the unchanged server projection again.
    rerender({ actionPending: false });
    await act(async () => {
      await Promise.resolve();
    });
    expect(result.current.pendingCardMove).toBeNull();
    expect(projectPendingBoardCardMove(stored, result.current.pendingCardMove).get(column.id)).toEqual([
      card,
    ]);
    // A successful server projection already contains the same destination card.
    expect(projectPendingBoardCardMove(optimistic, pending).get("destination")).toEqual([card]);
  });

  it("updates live status and actions without moving an eager card back to its source", () => {
    const pending = { card, targetColumnID: "destination", dropRect: new DOMRect(300, 0, 200, 100) };
    const running: KanbanCardVM = {
      ...card,
      statusKind: "running",
      actions: { ...card.actions, canInterrupt: true, canResume: false },
    };
    const projected = projectPendingBoardCardMove(
      new Map([
        [column.id, [running]],
        ["destination", []],
      ]),
      pending,
    );
    expect(projected.get(column.id)).toEqual([]);
    expect(projected.get("destination")).toEqual([running]);
    const confirmed = projectPendingBoardCardMove(
      new Map([
        [column.id, [card]],
        ["destination", [running]],
      ]),
      pending,
    );
    expect(confirmed.get("destination")).toEqual([running]);
  });

  it.each(["failure", "confirmation", "success"] as const)(
    "keeps the eager card during a start request and handles %s without losing the gesture",
    async (outcome) => {
      const services = createTestServices(startupRoutes);
      const response = deferred<TaskStartResponse>();
      const refresh = deferred<undefined>();
      vi.spyOn(services.api, "startTask").mockReturnValue(response.promise);
      const onActionError = vi.fn();
      const { result } = renderHook(
        () => {
          const action = useBoardInitiatingActionController({
            api: services.api,
            onActionError,
            onApplied: async () => refresh.promise,
            startErrorTitle: "start",
            moveErrorTitle: "move",
            refreshErrorTitle: "refresh",
            resumeErrorTitle: "resume",
          });
          const gesture = useBoardDragLifecycle({
            disabled: false,
            rootRef: { current: null },
            pendingTaskIDs: action.initiatingAction.pendingStartMoveTaskIDs,
            confirmationTaskID: action.initiatingAction.pending === null ? null : card.id,
          });
          return { action, gesture };
        },
        { wrapper: ({ children }) => <TestAppProviders services={services}>{children}</TestAppProviders> },
      );
      act(() => {
        result.current.gesture.start(drag);
      });
      act(() => {
        result.current.gesture.drop("destination", new DOMRect(300, 0, 200, 100));
        result.current.action.runCardAction(startTaskInitiatingAction(card.id));
      });
      expect(result.current.gesture.pendingCardMove?.targetColumnID).toBe("destination");
      const failure = new Error("backend rejected the move");
      await act(async () => {
        if (outcome === "failure") response.reject(failure);
        else if (outcome === "confirmation")
          response.resolve({
            outcome: "selection_required",
            selectionRequired: { reason: "policy_requires_selection" },
          });
        else response.resolve({ outcome: "applied", applied: { currentNodes: [] } });
        await response.promise.catch(() => undefined);
      });
      if (outcome === "failure") {
        await waitFor(() => {
          expect(result.current.gesture.pendingCardMove).toBeNull();
        });
        expect(onActionError).toHaveBeenCalledOnce();
        expect(onActionError.mock.calls[0]?.[2]).toBe(failure);
      } else if (outcome === "confirmation") {
        await waitFor(() => {
          expect(result.current.action.initiatingAction.pending).not.toBeNull();
        });
        expect(result.current.gesture.pendingCardMove?.targetColumnID).toBe("destination");
        act(() => {
          result.current.action.initiatingAction.close();
        });
        await waitFor(() => {
          expect(result.current.gesture.pendingCardMove).toBeNull();
        });
        expect(onActionError).not.toHaveBeenCalled();
      } else {
        expect(result.current.gesture.pendingCardMove?.targetColumnID).toBe("destination");
        await act(async () => {
          refresh.resolve(undefined);
          await refresh.promise;
        });
        await waitFor(() => {
          expect(result.current.gesture.pendingCardMove).toBeNull();
        });
        expect(onActionError).not.toHaveBeenCalled();
      }
    },
  );

  it.each(["destination", "outside", "escape", "cancel"] as const)(
    "finishes a pointer drag on %s without requiring native HTML drops",
    async (finish) => {
      const onStart = vi.fn();
      const onDrop = vi.fn();
      const onCancel = vi.fn();
      const onClick = vi.fn();
      const destination = { ...column, id: "column-2", name: "Done", taskCount: 0 };
      render(
        <DragDropSurface onDrop={onDrop} onCancel={onCancel}>
          {[column, destination].map((item) => (
            <KanbanColumn
              key={item.id}
              column={item}
              cards={item.id === column.id ? [card] : []}

              dragDisabled={false}
              dropState="idle"
              hasMoreCards={false}
              isFirstActive={false}
              isLoadingMoreCards={false}
              onCardClick={onClick}
              onCardDragStart={onStart}
              onDeleteTask={vi.fn()}
              onInterruptTask={vi.fn()}
              onLoadMoreCards={vi.fn()}
              onResumeTask={vi.fn()}
            />
          ))}
        </DragDropSurface>,
      );
      const source = screen.getByRole("article", { name: card.title });
      const sourceColumn = screen.getByRole("listitem", { name: column.name });
      const targetColumn = screen.getByRole("listitem", { name: destination.name });
      vi.spyOn(source, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 200, 100));
      vi.spyOn(sourceColumn, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 240, 600));
      vi.spyOn(targetColumn, "getBoundingClientRect").mockReturnValue(new DOMRect(300, 0, 240, 600));
      fireEvent.pointerDown(source, { button: 0, clientX: 50, clientY: 50 });
      fireEvent.pointerMove(document, { clientX: 100, clientY: 50 });
      await waitFor(() => {
        expect(onStart).toHaveBeenCalledWith(drag);
      });
      await waitFor(() => {
        expect(screen.getAllByTestId("task-card")).toHaveLength(2);
      });
      const clientX = finish === "outside" ? 650 : 350;
      fireEvent.pointerMove(document, { clientX, clientY: 50 });
      vi.useFakeTimers();
      if (finish === "escape") fireEvent.keyDown(document, { code: "Escape", key: "Escape" });
      else if (finish === "cancel") fireEvent.pointerCancel(document);
      else fireEvent.pointerUp(document, { clientX, clientY: 50 });
      fireEvent.click(source);
      expect(onClick).not.toHaveBeenCalled();
      await act(async () => {
        await vi.runOnlyPendingTimersAsync();
      });
      vi.useRealTimers();
      if (finish === "escape" || finish === "cancel") {
        expect(onCancel).toHaveBeenCalledOnce();
        expect(onDrop).not.toHaveBeenCalled();
      } else {
        expect(onDrop).toHaveBeenCalledOnce();
        expect(onDrop.mock.calls[0]?.[0]).toBe(finish === "outside" ? null : destination.id);
        expect(onCancel).not.toHaveBeenCalled();
      }
    },
  );

  it("keeps the Retry boundary in fixed column chrome outside the scroll content", () => {
    const boundary = {
      state: "error" as const,
      message: "message",
      retryLabel: "retry",
      onRetry: vi.fn(),
    };

    renderColumn(
      <KanbanColumn
        cards={[]}
        column={column}
        dragDisabled={false}
        dropState="idle"
        hasMoreCards={false}
        initialBoundary={undefined}
        isFirstActive
        isLoadingMoreCards={false}
        nextBoundary={undefined}
        onCardClick={vi.fn()}
        onCardDragStart={vi.fn()}
        onDeleteTask={vi.fn()}
        onInterruptTask={vi.fn()}
        onLoadMoreCards={vi.fn()}
        onResumeTask={vi.fn()}
        replacementBoundary={boundary}
      />,
    );

    expect(screen.getByRole("alert")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button"));
    expect(boundary.onRetry).toHaveBeenCalledOnce();
  });

  it("keeps server-authorized actions while an invalid Workflow disables drag", () => {
    const onCardDragStart = vi.fn();
    const onResumeTask = vi.fn();

    renderColumn(
      <KanbanColumn
        cards={[card]}
        column={column}
        dragDisabled
        dropState="idle"
        hasMoreCards={false}
        initialBoundary={undefined}
        isFirstActive
        isLoadingMoreCards={false}
        nextBoundary={undefined}
        onCardClick={vi.fn()}
        onCardDragStart={onCardDragStart}
        onDeleteTask={vi.fn()}
        onInterruptTask={vi.fn()}
        onLoadMoreCards={vi.fn()}
        onResumeTask={onResumeTask}
        replacementBoundary={undefined}
      />,
    );

    const renderedCard = screen.getByRole("article", { name: "Task" });
    fireEvent.pointerDown(renderedCard, { button: 0, clientX: 50, clientY: 50 });
    fireEvent.pointerMove(document, { clientX: 150, clientY: 50 });
    fireEvent.pointerUp(document, { clientX: 150, clientY: 50 });
    expect(onCardDragStart).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "board.resume" }));
    expect(onResumeTask).toHaveBeenCalledWith("task-1");
  });

  it("uses the queued warning control to force Resume", () => {
    const onResumeTask = vi.fn();
    renderColumn(
      <KanbanColumn
        cards={[{ ...card, statusKind: "queued" }]}
        column={column}
        dragDisabled={false}
        dropState="idle"
        hasMoreCards={false}
        initialBoundary={undefined}
        isFirstActive
        isLoadingMoreCards={false}
        nextBoundary={undefined}
        onCardClick={vi.fn()}
        onCardDragStart={vi.fn()}
        onDeleteTask={vi.fn()}
        onInterruptTask={vi.fn()}
        onLoadMoreCards={vi.fn()}
        onResumeTask={onResumeTask}
        replacementBoundary={undefined}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "board.waitingDueToConcurrencyLimits" }));
    expect(onResumeTask).toHaveBeenCalledWith("task-1");
    expect(screen.queryByRole("button", { name: "board.resume" })).not.toBeInTheDocument();
  });

  it("shows loading only for the pending Task action", () => {
    const secondCard = { ...card, id: "task-2", shortID: "KNT-2", title: "Second Task" };
    renderColumn(
      <KanbanColumn
        cards={[card, secondCard]}
        column={{ ...column, taskCount: 2 }}
        dragDisabled={false}
        dropState="idle"
        hasMoreCards={false}
        initialBoundary={undefined}
        isFirstActive
        isLoadingMoreCards={false}
        nextBoundary={undefined}
        onCardClick={vi.fn()}
        onCardDragStart={vi.fn()}
        onDeleteTask={vi.fn()}
        onInterruptTask={vi.fn()}
        onLoadMoreCards={vi.fn()}
        onResumeTask={vi.fn()}
        pendingResumeTaskIDs={new Set(["task-1"])}
        replacementBoundary={undefined}
      />,
    );

    const resumes = screen.getAllByRole("button", { name: "board.resume" });
    expect(resumes).toHaveLength(2);
    expect(resumes.filter((button) => button.getAttribute("aria-busy") === "true")).toHaveLength(1);
    for (const button of resumes) expect(button).toBeEnabled();
    expect(screen.queryByRole("button", { name: "board.interrupt" })).not.toBeInTheDocument();
  });

  it("does not restore a drag after the selected Workflow becomes invalid", async () => {
    const rootRef = { current: null };
    const { result, rerender } = renderHook(
      ({ disabled }) =>
        useBoardDragLifecycle({ disabled, rootRef, pendingTaskIDs: new Set(), confirmationTaskID: null }),
      {
        initialProps: { disabled: false },
      },
    );

    act(() => {
      result.current.start(drag);
    });
    expect(result.current.activeDrag).toBe(drag);

    rerender({ disabled: true });
    expect(result.current.activeDrag).toBeNull();

    await waitFor(() => {
      expect(result.current.dragBlocked).toBe(false);
    });
    rerender({ disabled: false });
    expect(result.current.activeDrag).toBeNull();
  });
});
