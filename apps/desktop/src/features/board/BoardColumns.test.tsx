import { fireEvent, render, screen, within } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { beforeAll, vi } from "vitest";

import { appI18n, initializeI18n } from "../../i18n/setup";
import { KanbanColumn } from "./BoardColumns";
import type { KanbanCardVM, KanbanColumnVM } from "./BoardColumnViewModel";
import { boardCardDragPayloadType, decodeBoardCardDragPayload } from "./BoardDragTypes";

describe("KanbanColumn", () => {
  beforeAll(async () => {
    await initializeI18n();
  });

  it("renders load-more with shared spinner and hidden accessible label", () => {
    render(
      <I18nextProvider i18n={appI18n}>
        <KanbanColumn
          actionsDisabled={false}
          cards={[card]}
          column={column}
          dropState="idle"
          hasMoreCards
          isFirstActive={false}
          isLoadingMoreCards
          onCardClick={() => undefined}
          onCardDragEnd={() => undefined}
          onCardDragStart={() => undefined}
          onDropTask={() => undefined}
          onInterruptTask={() => undefined}
          onLoadMoreCards={() => undefined}
          onResumeTask={() => undefined}
        />
      </I18nextProvider>,
    );

    expect(screen.getByRole("status", { name: "Loading more" })).toContainElement(
      screen.getByTestId("spinner"),
    );
    expect(screen.getByText("Loading more")).toHaveClass("sr-only");
  });

  it("keeps action buttons in the chip row, uses danger interrupt, and omits run count chip", () => {
    const onInterruptTask = vi.fn();
    const onCardClick = vi.fn();

    render(
      <I18nextProvider i18n={appI18n}>
        <KanbanColumn
          actionsDisabled={false}
          cards={[
            {
              ...card,
              actions: {
                ...card.actions,
                canInterrupt: true,
                interruptRunID: "run-1",
              },
            },
          ]}
          column={column}
          dropState="idle"
          hasMoreCards={false}
          isFirstActive={false}
          isLoadingMoreCards={false}
          onCardClick={onCardClick}
          onCardDragEnd={() => undefined}
          onCardDragStart={() => undefined}
          onDropTask={() => undefined}
          onInterruptTask={onInterruptTask}
          onLoadMoreCards={() => undefined}
          onResumeTask={() => undefined}
        />
      </I18nextProvider>,
    );

    const footer = screen.getByTestId("task-card-footer");
    expect(footer).toHaveClass("items-start", "justify-between");
    expect(screen.getByTestId("task-card-chips")).toHaveClass(
      "task-card-chip-row",
      "flex-wrap",
      "flex-1",
      "min-w-0",
    );
    expect(screen.getByTestId("task-card-chip-slot")).toHaveClass("task-card-chip-slot", "items-center");
    expect(screen.getByText("Main")).toBeInTheDocument();
    expect(screen.queryByText("Runs: 1")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open task detail" })).not.toBeInTheDocument();

    const interruptButton = within(footer).getByRole("button", { name: "Interrupt" });
    expect(interruptButton).toHaveStyle({
      "--button-border": "var(--color-error)",
      "--button-color": "var(--color-error)",
    });

    fireEvent.click(interruptButton);

    expect(onInterruptTask).toHaveBeenCalledWith("task-1", "run-1");
    expect(onCardClick).not.toHaveBeenCalled();
  });

  it("opens task detail when clicking any non-action area of the card", () => {
    const onCardClick = vi.fn();

    render(
      <I18nextProvider i18n={appI18n}>
        <KanbanColumn
          actionsDisabled={false}
          cards={[card]}
          column={column}
          dropState="idle"
          hasMoreCards={false}
          isFirstActive={false}
          isLoadingMoreCards={false}
          onCardClick={onCardClick}
          onCardDragEnd={() => undefined}
          onCardDragStart={() => undefined}
          onDropTask={() => undefined}
          onInterruptTask={() => undefined}
          onLoadMoreCards={() => undefined}
          onResumeTask={() => undefined}
        />
      </I18nextProvider>,
    );

    fireEvent.click(screen.getByText("Task"));
    fireEvent.click(screen.getByText("Body"));
    fireEvent.click(screen.getByTestId("task-card-footer"));
    const renderedCard = screen.getByRole("article", { name: "Task" });
    renderedCard.focus();
    expect(renderedCard).toHaveFocus();
    fireEvent.keyDown(renderedCard, { key: "Enter" });

    expect(onCardClick).toHaveBeenCalledTimes(4);
    expect(onCardClick).toHaveBeenCalledWith("task-1");
  });

  it("starts override drags for active cards without start or move targets", () => {
    const onCardDragStart = vi.fn();
    const onCardClick = vi.fn();

    render(
      <I18nextProvider i18n={appI18n}>
        <KanbanColumn
          actionsDisabled={false}
          cards={[
            {
              ...card,
              actions: {
                ...card.actions,
                canStart: false,
                manualMoveTargetNodeIDs: [],
              },
            },
          ]}
          column={column}
          dropState="blocked"
          hasMoreCards={false}
          isFirstActive={false}
          isLoadingMoreCards={false}
          onCardClick={onCardClick}
          onCardDragEnd={() => undefined}
          onCardDragStart={onCardDragStart}
          onDropTask={() => undefined}
          onInterruptTask={() => undefined}
          onLoadMoreCards={() => undefined}
          onResumeTask={() => undefined}
        />
      </I18nextProvider>,
    );

    const dataTransfer = new TestDataTransfer();
    const renderedCard = screen.getByRole("article", { name: "Task" });
    expect(renderedCard).toHaveAttribute("draggable", "true");
    expect(screen.getByRole("listitem", { name: "Backlog" })).toHaveAttribute("data-drop-state", "blocked");

    fireEvent.dragStart(renderedCard, { dataTransfer });

    expect(dataTransfer.getData("text/task-id")).toBe("task-1");
    expect(decodeBoardCardDragPayload(dataTransfer.getData(boardCardDragPayloadType))).toEqual({
      taskID: "task-1",
      canStart: false,
      activeNodeIDs: ["backlog"],
      statusKind: "backlog",
      manualMoveTargetNodeIDs: [],
    });
    expect(onCardDragStart).toHaveBeenCalledTimes(1);
    expect(onCardClick).not.toHaveBeenCalled();
  });

  it("accepts board-card dragover before drop-state rerenders from idle", () => {
    render(
      <I18nextProvider i18n={appI18n}>
        <KanbanColumn
          actionsDisabled={false}
          cards={[]}
          column={column}
          dropState="idle"
          hasMoreCards={false}
          isFirstActive={false}
          isLoadingMoreCards={false}
          onCardClick={() => undefined}
          onCardDragEnd={() => undefined}
          onCardDragStart={() => undefined}
          onDropTask={() => undefined}
          onInterruptTask={() => undefined}
          onLoadMoreCards={() => undefined}
          onResumeTask={() => undefined}
        />
      </I18nextProvider>,
    );

    const dataTransfer = new TestDataTransfer();
    dataTransfer.setData(boardCardDragPayloadType, "{}");
    const event = createCancelableDragEvent("dragover", dataTransfer);

    screen.getByRole("listitem", { name: "Backlog" }).dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(dataTransfer.dropEffect).toBe("move");
  });
});

class TestDataTransfer {
  readonly #values = new Map<string, string>();
  effectAllowed = "all";
  dropEffect = "none";

  get types(): readonly string[] {
    return [...this.#values.keys()];
  }

  setData(type: string, value: string): void {
    this.#values.set(type, value);
  }

  getData(type: string): string {
    return this.#values.get(type) ?? "";
  }
}

function createCancelableDragEvent(type: string, dataTransfer: TestDataTransfer): Event {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, "dataTransfer", { value: dataTransfer });
  return event;
}

const column: KanbanColumnVM = {
  assigneeRole: "",
  id: "backlog",
  name: "Backlog",
  taskCount: 1,
};

const card: KanbanCardVM = {
  activeNodeIDs: ["backlog"],
  actions: {
    canInterrupt: false,
    canResume: false,
    canStart: true,
    interruptRunID: "",
    manualMoveTargetNodeIDs: [],
    resumeRunID: "",
  },
  bodyPreview: "Body",
  id: "task-1",
  shortID: "T-1",
  sourceWorkspaceName: "Main",
  statusKind: "backlog",
  title: "Task",
  updatedAt: Date.UTC(2026, 0, 1),
};
