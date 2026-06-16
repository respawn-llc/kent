import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { vi } from "vitest";

import type { JsonValue } from "../../api/json";
import { App } from "../../App";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import { boardCardViewTransitionName } from "./BoardCardMotionModel";
import {
  boardColumnAt,
  boardNodeCardsResponse,
  boardResponse,
  boardRoutes,
  deferredBoardCardsResponse,
  doneBoardCard,
  DroppedPayloadDataTransfer,
  firstBoardCard,
  firstBoardGroup,
  installBoardRouteLifecycle,
  installIntersectionObserverMock,
  isObject,
  restoreStartViewTransition,
  taskActions,
  TestDataTransfer,
} from "./boardRouteTestHarness";

describe("BoardRoute drag and drop", () => {
  installBoardRouteLifecycle();

  it("renders workflow groups and drag-starts a Backlog task without confirmation", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.start", result: {} },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Core" })).toBeInTheDocument();
    expect(screen.queryByTestId("board-transition-source")).not.toBeInTheDocument();
    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const targetColumn = screen.getByRole("listitem", { name: "Implement" });
    const dataTransfer = new TestDataTransfer();

    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(targetColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.start",
        params: { task_id: "task-1" },
      });
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("starts tasks from in-memory drag state after rerender when browser dataTransfer drops custom payloads", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.start", result: {} },
    ]);

    const view = render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const targetColumn = screen.getByRole("listitem", { name: "Implement" });
    const dataTransfer = new DroppedPayloadDataTransfer();

    fireEvent.dragStart(card, { dataTransfer });
    view.rerender(<App services={services} />);
    fireEvent.drop(targetColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.start",
        params: { task_id: "task-1" },
      });
    });
  });

  it("accepts an override drop on a blocked target", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const dataTransfer = new TestDataTransfer();
    const doneColumn = screen.getByRole("listitem", { name: "Done" });
    fireEvent.dragStart(card, { dataTransfer });
    expect(doneColumn).toHaveAttribute("data-drop-state", "blocked");
    fireEvent.drop(doneColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "done",
          output_values: {},
          allow_missing_edge: true,
        },
      });
    });
  });

  it("waits for a count-dirty collapsed target column before animating a moved card", async () => {
    const visibility = installIntersectionObserverMock();
    const originalStartViewTransitionDescriptor = Object.getOwnPropertyDescriptor(document, "startViewTransition");
    const startViewTransition = vi.fn((update: () => void | Promise<void>) => {
      expect(screen.getByRole("article", { name: "Write focused tests" })).toHaveStyle({
        viewTransitionName: boardCardViewTransitionName("task-1"),
      });
      const updateCallbackDone = Promise.resolve(update());
      return {
        finished: updateCallbackDone,
        ready: Promise.resolve(),
        updateCallbackDone,
      };
    });
    Object.defineProperty(document, "startViewTransition", {
      configurable: true,
      value: startViewTransition,
    });
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const movableCard = {
      ...firstBoardCard(),
      actions: {
        ...firstBoardCard().actions,
        can_start: false,
        manual_move_target_node_ids: ["node-2"],
      },
    };
    const reconCard = {
      ...movableCard,
      active_node_ids: ["node-2"],
      status: {
        ...movableCard.status,
        kind: "running",
        label: "Running",
        native_state: "running",
        node_ids: ["node-2"],
      },
    };
    const reconColumn = {
      node: {
        node_id: "node-2",
        key: "recon",
        kind: "agent",
        display_name: "Recon",
        assignee_role: "analyst",
      },
      group_id: "group-1",
      sort_order: 2,
      is_backlog: false,
      is_done: false,
      task_count: 0,
    };
    const initialBoard = {
      board: {
        ...boardResponse.board,
        cards: [movableCard],
        columns: [boardColumnAt(0), boardColumnAt(1), reconColumn, boardColumnAt(2)],
        groups: [{ ...firstBoardGroup(), node_ids: ["node-1", "node-2"] }],
      },
    };
    const movedBoard = {
      board: {
        ...initialBoard.board,
        columns: [
          { ...boardColumnAt(0), task_count: 0 },
          boardColumnAt(1),
          { ...reconColumn, task_count: 1 },
          boardColumnAt(2),
        ],
      },
    };
    const reconCards = deferredBoardCardsResponse();
    let backlogCardCalls = 0;
    const nodeCardCalls: string[] = [];
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        handler: (_params, callIndex) => (callIndex === 0 ? initialBoard : movedBoard),
      },
      {
        method: "workflow.board.nodeCards.list",
        handler: async (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          nodeCardCalls.push(nodeID);
          if (nodeID === "backlog") {
            backlogCardCalls += 1;
            return boardNodeCardsResponse(nodeID, backlogCardCalls === 1 ? [movableCard] : [], "");
          }
          if (nodeID === "node-2") {
            return reconCards.promise;
          }
          return boardNodeCardsResponse(nodeID, [], "");
        },
      },
      { method: "workflow.task.move", result: {} },
    ]);

    try {
      render(<App services={services} />);

      expect(await screen.findByRole("heading", { name: "Backlog" })).toBeInTheDocument();
      act(() => {
        visibility.reveal("Backlog");
        visibility.reveal("Recon");
      });
      const card = await screen.findByRole("article", { name: "Write focused tests" });
      act(() => {
        visibility.reveal("Write focused tests");
      });
      startViewTransition.mockClear();

      fireEvent.dragStart(card, { dataTransfer: new TestDataTransfer() });
      fireEvent.drop(screen.getByRole("listitem", { name: "Recon" }), { dataTransfer: new TestDataTransfer() });

      await waitFor(() => {
        expect(nodeCardCalls).toContain("node-2");
      });
      expect(startViewTransition).not.toHaveBeenCalled();

      await act(async () => {
        reconCards.resolve(boardNodeCardsResponse("node-2", [reconCard], ""));
      });
      await waitFor(() => {
        expect(startViewTransition).toHaveBeenCalledOnce();
      });
    } finally {
      restoreStartViewTransition(originalStartViewTransitionDescriptor);
    }
  });

  it("uses server manual-move target permissions and card action flags", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const baseCard = firstBoardCard();
    const activeCard = {
      ...baseCard,
      active_node_ids: ["node-1"],
      status: { ...baseCard.status, kind: "active", label: "Active", node_ids: ["node-1"] },
      actions: {
        ...taskActions,
        can_start: false,
        can_interrupt: true,
        interrupt_run_id: "run-1",
        manual_move_target_node_ids: ["done"],
      },
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(boardResponse, {
        backlog: { cards: [] },
        "node-1": { cards: [activeCard] },
        done: { cards: [] },
      }),
      { method: "workflow.task.move", result: {} },
      { method: "workflow.task.interrupt", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    fireEvent.click(screen.getByRole("button", { name: "Interrupt" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.interrupt",
        params: { task_id: "task-1", run_id: "run-1" },
      });
    });

    const doneColumn = screen.getByRole("listitem", { name: "Done" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    expect(doneColumn).toHaveAttribute("data-drop-state", "allowed");
    fireEvent.drop(doneColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: { task_id: "task-1", target_node_id: "done", output_values: {} },
      });
    });
  });

  it("moves a Done card back to Backlog without confirmation", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const doneCard = doneBoardCard();
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.is_done ? { ...column, task_count: 1 } : column,
            ),
          },
        },
        {
          backlog: { cards: [] },
          "node-1": { cards: [] },
          done: { cards: [doneCard] },
        },
      ),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    expect(card).toHaveAttribute("draggable", "true");
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(screen.getByRole("listitem", { name: "Backlog" }), { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "backlog",
          output_values: {},
          allow_missing_edge: true,
        },
      });
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("confirms a Done rollback that starts an agent", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const doneCard = doneBoardCard();
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.is_done ? { ...column, task_count: 1 } : column,
            ),
          },
        },
        {
          backlog: { cards: [] },
          "node-1": { cards: [] },
          done: { cards: [doneCard] },
        },
      ),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const implementColumn = screen.getByRole("listitem", { name: "Implement" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    expect(implementColumn).toHaveAttribute("data-drop-state", "blocked");
    fireEvent.drop(implementColumn, { dataTransfer });

    const dialog = await screen.findByRole("dialog", { name: "Rollback and start the agent?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(false);

    fireEvent.dragStart(card, { dataTransfer: new TestDataTransfer() });
    fireEvent.drop(implementColumn, { dataTransfer: new TestDataTransfer() });
    fireEvent.click(await screen.findByRole("button", { name: "Rollback and start agent" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "node-1",
          output_values: {},
          auto_approve: true,
        },
      });
    });
  });

  it("collects missing inputs before an override drop that starts an agent", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const codeReviewColumn = {
      node: {
        node_id: "node-2",
        key: "code_review",
        kind: "agent",
        display_name: "Code review",
        assignee_role: "reviewer",
        transition_output_fields: [{ name: "summary", description: "Prior work summary." }],
      },
      group_id: "group-1",
      sort_order: 2,
      is_backlog: false,
      is_done: false,
      task_count: 0,
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          groups: [
            {
              ...firstBoardGroup(),
              node_ids: ["node-1", "node-2"],
            },
          ],
          columns: [boardColumnAt(0), boardColumnAt(1), codeReviewColumn, boardColumnAt(2)],
        },
      }),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const codeReview = screen.getByRole("listitem", { name: "Code review" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    expect(codeReview).toHaveAttribute("data-drop-state", "blocked");
    fireEvent.drop(codeReview, { dataTransfer });

    const dialog = await screen.findByRole("dialog", { name: "Submit missing inputs" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(false);

    fireEvent.dragStart(card, { dataTransfer: new TestDataTransfer() });
    fireEvent.drop(codeReview, { dataTransfer: new TestDataTransfer() });
    fireEvent.change(await screen.findByLabelText("summary"), { target: { value: "Replacement summary" } });
    fireEvent.click(screen.getByRole("button", { name: "Submit and start agent" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "node-2",
          output_values: { summary: "Replacement summary" },
          allow_missing_edge: true,
          auto_approve: true,
        },
      });
    });
  });

  it("shows a toast when an allowed card drop fails on the server", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const activeCard = {
      ...firstBoardCard(),
      active_node_ids: ["node-1"],
      status: { ...firstBoardCard().status, kind: "active", label: "Active", node_ids: ["node-1"] },
      actions: {
        ...taskActions,
        can_start: false,
        manual_move_target_node_ids: ["done"],
      },
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(boardResponse, {
        backlog: { cards: [] },
        "node-1": { cards: [activeCard] },
        done: { cards: [] },
      }),
      { method: "workflow.task.move", error: new Error("required output summary") },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(screen.getByRole("listitem", { name: "Done" }), { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(true);
    });
  });

  it("shows a toast when a run created by a card move is interrupted after refresh", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const activeCard = {
      ...firstBoardCard(),
      active_node_ids: ["node-1"],
      status: { ...firstBoardCard().status, kind: "active", label: "Active", node_ids: ["node-1"] },
      actions: {
        ...taskActions,
        can_start: false,
        manual_move_target_node_ids: ["done"],
      },
    };
    const interruptedCard = {
      ...activeCard,
      active_node_ids: ["done"],
      status: {
        ...activeCard.status,
        kind: "interrupted",
        label: "Interrupted",
        native_state: "interrupted",
        node_ids: ["done"],
        run_ids: ["run-move"],
        attention_types: ["interrupted_run"],
      },
    };
    const initialBoard = {
      board: {
        ...boardResponse.board,
        columns: boardResponse.board.columns.map((column) =>
          column.node.node_id === "node-1" ? { ...column, task_count: 1 } : column,
        ),
      },
    };
    const movedBoard = {
      board: {
        ...initialBoard.board,
        columns: initialBoard.board.columns.map((column) => {
          if (column.node.node_id === "node-1") {
            return { ...column, task_count: 0 };
          }
          if (column.node.node_id === "done") {
            return { ...column, task_count: 1 };
          }
          return column;
        }),
      },
    };
    let nodeCardsAfterMove = false;
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        handler: (_params, callIndex) => (callIndex === 0 ? initialBoard : movedBoard),
      },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          if (nodeID === "node-1") {
            return boardNodeCardsResponse(nodeID, nodeCardsAfterMove ? [] : [activeCard], "");
          }
          if (nodeID === "done") {
            return boardNodeCardsResponse(nodeID, nodeCardsAfterMove ? [interruptedCard] : [], "");
          }
          return boardNodeCardsResponse(nodeID, [], "");
        },
      },
      {
        method: "workflow.task.move",
        handler: () => {
          nodeCardsAfterMove = true;
          return { run_ids: ["run-move"], state: "approved", transition_id: "transition-move" };
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Implement" })).toBeInTheDocument();
    act(() => {
      visibility.reveal("Implement");
      visibility.reveal("Done");
    });
    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(screen.getByRole("listitem", { name: "Done" }), { dataTransfer });

    expect(await screen.findByText("Workflow run interrupted")).toBeInTheDocument();
    expect(screen.getByText(/run-move/u)).toBeInTheDocument();
  });
});
