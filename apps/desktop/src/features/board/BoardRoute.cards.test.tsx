import { type NativeDialogWindowOptions } from "@app/native-bridge";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import type { JsonValue } from "../../api/json";
import { App } from "../../App";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import {
  type BoardRouteCard,
  boardNodeCardsResponse,
  boardResponse,
  boardRoutes,
  emptyActivityResponse,
  firstBoardCard,
  installBoardRouteLifecycle,
  installIntersectionObserverMock,
  isObject,
  nativeDialogBridge,
  setScrollMetrics,
  taskActions,
  taskDetailResponseForCancel,
} from "./boardRouteTestHarness";

describe("BoardRoute cards", () => {
  installBoardRouteLifecycle();

  it("deletes a task from the card context menu", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1&taskId=task-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.get", result: taskDetailResponseForCancel() },
      { method: "workflow.task.activity.list", result: emptyActivityResponse },
      { method: "workflow.task.delete", result: {} },
    ]);

    render(<App services={services} />);

    fireEvent.contextMenu(await screen.findByRole("article", { name: "Write focused tests" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
    expect(services.transport.calls.some((call) => call.method === "workflow.task.delete")).toBe(false);

    const dialog = await screen.findByRole("dialog", { name: "Delete task?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.delete",
        params: { task_id: "task-1" },
      });
    });
    await waitFor(() => {
      expect(new URLSearchParams(window.location.search).get("taskId")).toBe("");
    });
  });

  it("opens native confirmation before deleting a task when dialog windows are available", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices([...startupRoutes, ...boardRoutes()], nativeDialogBridge(opened));

    render(<App services={services} />);

    fireEvent.contextMenu(await screen.findByRole("article", { name: "Write focused tests" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));

    await waitFor(() => {
      expect(opened).toContainEqual(
        expect.objectContaining({
          params: { taskID: "task-1" },
          route: "/native-dialog/task-delete",
          title: "Delete task?",
        }),
      );
    });
    expect(services.transport.calls.some((call) => call.method === "workflow.task.delete")).toBe(false);
  });

  it("loads node card pages only after columns become visible", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const nodeCardCalls: string[] = [];
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", result: boardResponse },
      {
        method: "workflow.board.nodeCards.list",
        handler: async (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          nodeCardCalls.push(nodeID);
          return boardNodeCardsResponse(nodeID, nodeID === "backlog" ? [firstBoardCard()] : [], "");
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Backlog" })).toBeInTheDocument();
    expect(nodeCardCalls).toEqual([]);

    act(() => {
      visibility.reveal("Backlog");
    });
    expect(await screen.findByRole("article", { name: "Write focused tests" })).toBeInTheDocument();
    expect(nodeCardCalls).toEqual(["backlog"]);

    act(() => {
      visibility.reveal("Implement");
    });
    await waitFor(() => {
      expect(nodeCardCalls).toEqual(["backlog", "node-1"]);
    });
  });

  it("loads Done node cards after Done becomes visible", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const nodeCardCalls: string[] = [];
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        result: {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.is_done ? { ...column, task_count: 1 } : column,
            ),
          },
        },
      },
      {
        method: "workflow.board.nodeCards.list",
        handler: async (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          nodeCardCalls.push(nodeID);
          return boardNodeCardsResponse(nodeID, [], "");
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Done" })).toBeInTheDocument();
    act(() => {
      visibility.reveal("Done");
    });
    await waitFor(() => {
      expect(nodeCardCalls).toContain("done");
    });
  });

  it("collapses empty non-starting columns by default and skips card loading until expanded", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const nodeCardCalls: string[] = [];
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", result: boardResponse },
      {
        method: "workflow.board.nodeCards.list",
        handler: async (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          nodeCardCalls.push(nodeID);
          return boardNodeCardsResponse(nodeID, [], "");
        },
      },
    ]);

    render(<App services={services} />);

    const startingColumn = await screen.findByRole("listitem", { name: "Implement" });
    const emptyDoneColumn = screen.getByRole("listitem", { name: "Done" });

    expect(startingColumn).toHaveAttribute("data-collapsed", "false");
    expect(within(startingColumn).getByTestId("kanban-column-task-count-node-1")).toBeInTheDocument();
    expect(emptyDoneColumn).toHaveAttribute("data-collapsed", "true");
    expect(within(emptyDoneColumn).queryByTestId("kanban-column-task-count-done")).not.toBeInTheDocument();

    act(() => {
      visibility.reveal("Done");
    });
    expect(nodeCardCalls).toEqual([]);

    fireEvent.click(within(emptyDoneColumn).getByRole("button", { name: "Expand Done" }));
    expect(emptyDoneColumn).toHaveAttribute("data-collapsed", "false");

    await waitFor(() => {
      expect(nodeCardCalls).toEqual(["done"]);
    });
  });

  it("renders pending-approval tasks in their source node column", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const pendingApprovalCard = {
      ...firstBoardCard(),
      short_id: "BUI-7",
      title: "Waiting on approval",
      body_preview: "Approval should remain visible on the board.",
      active_node_ids: ["node-1"],
      status: {
        ...firstBoardCard().status,
        kind: "waiting_approval",
        label: "Approval",
        native_state: "waiting_approval",
        node_ids: ["node-1"],
        attention_types: ["approval"],
      },
      actions: {
        ...taskActions,
        can_start: false,
        manual_move_target_node_ids: [],
      },
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.node.node_id === "node-1" ? { ...column, task_count: 1 } : { ...column, task_count: 0 },
            ),
            cards: [],
          },
        },
        {
          backlog: { cards: [] },
          "node-1": { cards: [pendingApprovalCard] },
          done: { cards: [] },
        },
      ),
    ]);

    render(<App services={services} />);

    const implementColumn = await screen.findByRole("listitem", { name: "Implement" });
    act(() => {
      visibility.reveal("Implement");
    });
    expect(
      await within(implementColumn).findByRole("article", { name: "Waiting on approval" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("article", { name: "Write focused tests" })).not.toBeInTheDocument();
  });

  it("refreshes node-card pages after task cancel so task moves from Backlog to Done", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    let canceled = false;
    const canceledCard: BoardRouteCard = {
      ...firstBoardCard(),
      active_node_ids: ["done"],
      status: {
        attention_types: [],
        kind: "canceled",
        label: "Canceled",
        native_state: "canceled",
        node_ids: ["done"],
        run_ids: [],
      },
      actions: { ...taskActions, can_start: false, can_cancel: false },
    };
    const boardWithCancelState = () => ({
      board: {
        ...boardResponse.board,
        columns: boardResponse.board.columns.map((column) => {
          if (column.is_backlog) {
            return { ...column, task_count: canceled ? 0 : 1 };
          }
          if (column.is_done) {
            return { ...column, task_count: canceled ? 1 : 0 };
          }
          return column;
        }),
      },
    });
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", handler: () => boardWithCancelState() },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          const cards =
            nodeID === "backlog" && !canceled
              ? [firstBoardCard()]
              : nodeID === "done" && canceled
                ? [canceledCard]
                : [];
          return boardNodeCardsResponse(nodeID, cards, "");
        },
      },
      { method: "workflow.task.get", result: taskDetailResponseForCancel() },
      { method: "workflow.task.activity.list", result: emptyActivityResponse },
      {
        method: "workflow.task.cancel",
        handler: () => {
          canceled = true;
          return {};
        },
      },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    fireEvent.click(card);
    fireEvent.click(await screen.findByRole("button", { name: "Cancel task" }));
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(
        within(screen.getByTestId("kanban-column-scroll-backlog")).queryByRole("article", {
          name: "Write focused tests",
        }),
      ).not.toBeInTheDocument();
    });

    await waitFor(() => {
      expect(
        within(screen.getByTestId("kanban-column-scroll-done")).getByRole("article", {
          name: "Write focused tests",
        }),
      ).toBeInTheDocument();
    });
    expect(services.transport.calls).toContainEqual({
      method: "workflow.task.cancel",
      params: { task_id: "task-1" },
    });
  });

  it("fetches the next board task page when a column scroll reaches the end", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const secondPageCard = {
      ...firstBoardCard(),
      task_id: "task-2",
      short_id: "T-2",
      title: "Second page task",
    };
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", result: boardResponse },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const pageToken =
            isObject(params) && typeof params.page_token === "string" ? params.page_token : "";
          return boardNodeCardsResponse(
            "backlog",
            pageToken === "cursor-2" ? [secondPageCard] : [firstBoardCard()],
            pageToken === "cursor-2" ? "" : "cursor-2",
          );
        },
      },
    ]);

    render(<App services={services} />);

    const scroller = await screen.findByTestId("kanban-column-scroll-backlog");
    await screen.findByRole("article", { name: "Write focused tests" });
    setScrollMetrics(scroller, { clientHeight: 100, scrollHeight: 140, scrollTop: 40 });
    fireEvent.scroll(scroller);

    expect(await screen.findByRole("article", { name: "Second page task" })).toBeInTheDocument();
    expect(services.transport.calls).toContainEqual({
      method: "workflow.board.nodeCards.list",
      params: {
        project_id: "project-1",
        workflow_id: "workflow-1",
        node_id: "backlog",
        page_size: 100,
        page_token: "cursor-2",
      },
    });
  });
});
