import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import { initializeI18n } from "../../i18n/setup";
import { WorkflowGraphCanvas, WorkflowNodeInfoTooltipContent } from "./WorkflowGraphCanvas";
import { groupIDFromPoint } from "./workflowGraphCanvasInteractions";
import type { WorkflowGraphNode } from "./workflowGraphLayout";

void initializeI18n();

describe("WorkflowGraphCanvas", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
  });

  it("renders graph nodes with kind-colored full outlines and opens inspectors for editable nodes", async () => {
    const copied: string[] = [];
    const onNodeInspect = vi.fn();
    const { unmount } = render(
      <WorkflowGraphCanvas
        graph={{
          edges: [],
          nodes: [
            workflowGroupGraphNode({ hasError: false, id: "group", label: "Group", x: -140 }),
            workflowGraphNode({
              hasError: false,
              id: "start",
              kind: "start",
              label: "Backlog",
              nodeKey: "backlog",
              x: 0,
            }),
            workflowGraphNode({ hasError: false, id: "agent", kind: "agent", label: "Agent", x: 120 }),
            workflowGraphNode({
              hasError: false,
              id: "terminal",
              kind: "terminal",
              label: "Done",
              nodeKey: "done",
              x: 240,
            }),
            workflowGraphNode({
              hasError: false,
              id: "join",
              kind: "join",
              label: "Join",
              type: "workflowJoin",
              x: 360,
            }),
            workflowGraphNode({ hasError: true, id: "error", kind: "agent", label: "Broken", x: 360 }),
          ],
        }}
        onCopyText={(value) => {
          copied.push(value);
        }}
        onEdgeInspect={() => undefined}
        onGroupInspect={() => undefined}
        onNodeInspect={onNodeInspect}
        onWorkflowInspect={() => undefined}
      />,
    );

    expect(screen.getByTestId("workflow-graph-group-group")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-outline)",
    });
    expect(screen.getByTestId("workflow-graph-group-group")).toHaveClass("island-surface-1", "nopan");
    expect(screen.getByTestId("workflow-graph-node-start")).toHaveAttribute("data-kind", "start");
    expect(screen.getByTestId("workflow-graph-node-start")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-primary)",
    });
    expect(within(screen.getByTestId("workflow-graph-node-start")).queryAllByTestId("workflow-node-target-handle")).toHaveLength(0);
    expect(within(screen.getByTestId("workflow-graph-node-start")).queryAllByTestId("workflow-node-source-handle")).toHaveLength(1);
    expect(screen.getByTestId("workflow-graph-node-agent")).toHaveAttribute("data-kind", "agent");
    expect(screen.getByTestId("workflow-graph-node-agent")).toHaveClass("island-surface-1", "nopan", "cursor-grab");
    expect(screen.getByTestId("workflow-graph-node-agent")).not.toHaveAttribute("draggable", "true");
    expect(screen.getByTestId("workflow-graph-node-agent")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-outline)",
    });
    expect(screen.getByTestId("workflow-graph-node-terminal")).toHaveAttribute("data-kind", "terminal");
    expect(screen.getByTestId("workflow-graph-node-terminal")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-success)",
    });
    expect(screen.getByTestId("workflow-graph-node-join")).toHaveAttribute("data-kind", "join");
    expect(screen.getByTestId("workflow-graph-node-join")).toHaveClass("island-surface-1");
    expect(screen.getByTestId("workflow-graph-node-join")).not.toHaveAttribute("draggable", "true");
    expect(screen.getByTestId("workflow-graph-node-join")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-secondary)",
    });
    expect(screen.getByTestId("workflow-graph-node-error")).toHaveClass("workflow-editor-node-error");
    expect(screen.getByTestId("workflow-graph-node-error")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-error)",
    });
    fireEvent.click(screen.getByTestId("workflow-graph-node-start"));
    expect(onNodeInspect).toHaveBeenLastCalledWith("start");
    fireEvent.click(screen.getByTestId("workflow-graph-node-terminal"));
    expect(onNodeInspect).toHaveBeenLastCalledWith("terminal");
    fireEvent.click(screen.getByTestId("workflow-graph-node-join"));
    expect(onNodeInspect).toHaveBeenCalledWith("join");
    fireEvent.click(screen.getByTestId("workflow-graph-node-agent"));
    expect(onNodeInspect).toHaveBeenCalledWith("agent");
    fireEvent.click(within(screen.getByTestId("workflow-graph-node-agent")).getByTestId("workflow-node-source-handle"));
    expect(onNodeInspect).toHaveBeenLastCalledWith("agent");
    for (const element of [
      screen.getByTestId("workflow-graph-group-group"),
      screen.getByTestId("workflow-graph-node-start"),
      screen.getByTestId("workflow-graph-node-agent"),
      screen.getByTestId("workflow-graph-node-terminal"),
      screen.getByTestId("workflow-graph-node-join"),
      screen.getByTestId("workflow-graph-node-error"),
    ]) {
      expect(element.className).not.toContain("border-l");
    }

    fireEvent.pointerMove(screen.getByTestId("workflow-graph-node-join"), { pointerType: "mouse" });
    await waitFor(() => {
      expect(screen.getByTestId("workflow-node-metadata-tooltip")).toHaveClass("w-[420px]");
    });
    expect(screen.getByTestId("workflow-node-metadata-tooltip")).toHaveClass(
      "max-w-[calc(100vw-var(--space-4)*2)]",
    );
    fireEvent.pointerMove(screen.getByTestId("workflow-graph-node-start"), { pointerType: "mouse" });
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Copy Key backlog" })).not.toBeInTheDocument();
    });
    fireEvent.pointerMove(screen.getByTestId("workflow-graph-node-terminal"), { pointerType: "mouse" });
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Copy Key done" })).not.toBeInTheDocument();
    });
    expect(screen.getByTestId("workflow-editor-tools")).toHaveClass(
      "island-surface-3",
      "fixed",
      "left-[var(--space-2)]",
      "top-[calc(var(--native-titlebar-height)+var(--space-2))]",
      "z-30",
    );
    expect(screen.getAllByTitle("Drag node to group")).toHaveLength(2);

    unmount();
    const longNodeID = "node_0123456789abcdef0123456789abcdef";
    render(
      <WorkflowNodeInfoTooltipContent
        nodeID={longNodeID}
        nodeKey="backlog"
        onCopyText={(value) => {
          copied.push(value);
        }}
      />,
    );
    expect(screen.getByText(longNodeID)).toHaveClass("break-all");
    expect(screen.getByText(longNodeID)).not.toHaveClass("truncate");
    fireEvent.click(screen.getByRole("button", { name: "Copy Key backlog" }));
    fireEvent.click(screen.getByRole("button", { name: `Copy ID ${longNodeID}` }));
    expect(copied).toEqual(["backlog", longNodeID]);
  });

  it("adds nodes from the canvas toolbar and reserves plain plus for add", () => {
    const onAddNode = vi.fn();
    render(
      <WorkflowGraphCanvas
        graph={{ edges: [], nodes: [] }}
        onAddNode={onAddNode}
        onEdgeInspect={() => undefined}
        onGroupInspect={() => undefined}
        onNodeInspect={() => undefined}
        onWorkflowInspect={() => undefined}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add node" }));
    fireEvent.click(screen.getByRole("button", { name: "Agent node" }));
    fireEvent.click(screen.getByRole("button", { name: "Add node" }));
    fireEvent.click(screen.getByRole("button", { name: "Terminal node" }));

    expect(onAddNode).toHaveBeenNthCalledWith(1, "agent");
    expect(onAddNode).toHaveBeenNthCalledWith(2, "terminal");
    expect(screen.getByRole("button", { name: "Zoom in" })).toBeEnabled();
  });

  it("selects graph entities for keyboard deletion and hides terminal source handles", () => {
    const onDeleteSelection = vi.fn();
    render(
      <WorkflowGraphCanvas
        graph={{
          edges: [],
          nodes: [
            workflowGraphNode({ hasError: false, id: "agent", kind: "agent", label: "Agent", x: 0 }),
            workflowGraphNode({ hasError: false, id: "terminal", kind: "terminal", label: "Done", x: 160 }),
          ],
        }}
        onDeleteSelection={onDeleteSelection}
        onEdgeInspect={() => undefined}
        onGroupInspect={() => undefined}
        onNodeInspect={() => undefined}
        onWorkflowInspect={() => undefined}
      />,
    );

    fireEvent.click(screen.getByTestId("workflow-graph-node-agent"));
    fireEvent.keyDown(window, { key: "Delete" });

    expect(onDeleteSelection).toHaveBeenCalledWith({ kind: "node", nodeID: "agent" });
    expect(within(screen.getByTestId("workflow-graph-node-agent")).queryAllByTestId("workflow-node-source-handle")).toHaveLength(1);
    expect(within(screen.getByTestId("workflow-graph-node-terminal")).queryAllByTestId("workflow-node-source-handle")).toHaveLength(0);
  });

  it("creates node groups from context menu and drag-drops nodes onto groups", async () => {
    const onAddNodeToGroup = vi.fn();
    const onCreateNodeGroup = vi.fn();
    const onNodeInspect = vi.fn();
    render(
      <WorkflowGraphCanvas
        graph={{
          edges: [],
          nodes: [
            workflowGroupGraphNode({ hasError: false, id: "group", label: "Group", x: -140 }),
            workflowGraphNode({ hasError: false, id: "agent", kind: "agent", label: "Agent", x: 0 }),
          ],
        }}
        onAddNodeToGroup={onAddNodeToGroup}
        onCreateNodeGroup={onCreateNodeGroup}
        onEdgeInspect={() => undefined}
        onGroupInspect={() => undefined}
        onNodeInspect={onNodeInspect}
        onWorkflowInspect={() => undefined}
      />,
    );

    fireEvent.contextMenu(screen.getByTestId("workflow-graph-node-agent"), { clientX: 24, clientY: 32 });
    fireEvent.click(screen.getByRole("menuitem", { name: "Create node group" }));
    expect(onCreateNodeGroup).toHaveBeenCalledWith("agent");

    const card = screen.getByTestId("workflow-graph-node-agent");
    const eventView = card.ownerDocument.defaultView;
    if (eventView === null) {
      throw new Error("Expected test document to have a default window");
    }
    const elementFromPoint = vi.fn<typeof document.elementFromPoint>(() => card);
    Object.defineProperty(document, "elementFromPoint", {
      configurable: true,
      value: elementFromPoint,
    });
    dispatchMouseEvent(within(card).getByTestId("workflow-node-source-handle"), eventView, "mousedown", {
      button: 0,
      clientX: 12,
      clientY: 18,
    });
    dispatchMouseEvent(eventView, eventView, "mousemove", { buttons: 1, clientX: 30, clientY: 34 });
    dispatchMouseEvent(eventView, eventView, "mouseup", { clientX: 36, clientY: 40 });
    expect(screen.queryByTestId("workflow-group-drag-preview")).not.toBeInTheDocument();
    expect(onAddNodeToGroup).not.toHaveBeenCalled();

    dispatchMouseEvent(card, eventView, "mousedown", { button: 0, clientX: 12, clientY: 18 });
    dispatchMouseEvent(eventView, eventView, "mousemove", { buttons: 1, clientX: 24, clientY: 28 });
    dispatchMouseEvent(eventView, eventView, "mousemove", { buttons: 1, clientX: 30, clientY: 34 });
    await waitFor(() => {
      expect(screen.getByTestId("workflow-group-drag-preview")).toHaveTextContent("Agent");
    });
    Object.defineProperty(screen.getByTestId("workflow-graph-group-group"), "getBoundingClientRect", {
      configurable: true,
      value: () => new eventView.DOMRect(0, 0, 100, 100),
    });
    expect(groupIDFromPoint(36, 40)).toBe("group");
    dispatchMouseEvent(eventView, eventView, "mouseup", { clientX: 36, clientY: 40 });

    expect(elementFromPoint).toHaveBeenCalledWith(36, 40);
    expect(onAddNodeToGroup).toHaveBeenCalledWith("agent", "group");
    expect(screen.queryByTestId("workflow-group-drag-preview")).not.toBeInTheDocument();

    fireEvent.click(card);
    expect(onNodeInspect).toHaveBeenCalledWith("agent");
  });
});

class MockResizeObserver implements ResizeObserver {
  observe(): void {
    return;
  }

  unobserve(): void {
    return;
  }

  disconnect(): void {
    return;
  }
}

function dispatchMouseEvent(
  target: Document | Element | Window,
  view: Window & typeof globalThis,
  type: "mousedown" | "mousemove" | "mouseup",
  options: MouseEventInit,
): void {
  const event = new view.MouseEvent(type, { bubbles: true, cancelable: true, ...options });
  Object.defineProperty(event, "view", { value: view });
  fireEvent(target, event);
}

function workflowGraphNode({
  id,
  label,
  kind,
  hasError,
  nodeKey = id,
  x,
  type = "workflowNode",
}: Readonly<{
  hasError: boolean;
  id: string;
  kind: string;
  label: string;
  nodeKey?: string;
  type?: string;
  x: number;
}>): WorkflowGraphNode {
  return {
    data: {
      entityID: id,
      entityKind: "node",
      groupID: "",
      hasError,
      key: nodeKey,
      kind,
      label,
      role: kind === "agent" ? "coder" : "",
    },
    draggable: kind === "agent",
    id,
    position: { x, y: 0 },
    style: { height: 92, width: 220 },
    type,
  };
}

function workflowGroupGraphNode({
  id,
  label,
  hasError,
  x,
}: Readonly<{
  hasError: boolean;
  id: string;
  label: string;
  x: number;
}>): WorkflowGraphNode {
  return {
    data: {
      empty: true,
      entityID: id,
      entityKind: "group",
      hasError,
      kind: "group",
      label,
    },
    draggable: false,
    id,
    position: { x, y: 140 },
    style: { height: 140, width: 260 },
    type: "workflowGroup",
  };
}
