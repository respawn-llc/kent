import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import { initializeI18n } from "../../i18n/setup";
import { WorkflowGraphCanvas, WorkflowNodeInfoTooltipContent } from "./WorkflowGraphCanvas";
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
    expect(screen.getByTestId("workflow-graph-node-start")).toHaveAttribute("data-kind", "start");
    expect(screen.getByTestId("workflow-graph-node-start")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-primary)",
    });
    expect(screen.getByTestId("workflow-graph-node-agent")).toHaveAttribute("data-kind", "agent");
    expect(screen.getByTestId("workflow-graph-node-agent")).toHaveClass("bg-[var(--color-island-3)]");
    expect(screen.getByTestId("workflow-graph-node-agent")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-outline)",
    });
    expect(screen.getByTestId("workflow-graph-node-terminal")).toHaveAttribute("data-kind", "terminal");
    expect(screen.getByTestId("workflow-graph-node-terminal")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-success)",
    });
    expect(screen.getByTestId("workflow-graph-node-join")).toHaveAttribute("data-kind", "join");
    expect(screen.getByTestId("workflow-graph-node-join")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-secondary)",
    });
    expect(screen.getByTestId("workflow-graph-node-error")).toHaveClass("workflow-editor-node-error");
    expect(screen.getByTestId("workflow-graph-node-error")).toHaveStyle({
      "--workflow-editor-node-outline-color": "var(--color-error)",
    });
    fireEvent.click(screen.getByTestId("workflow-graph-node-start"));
    fireEvent.click(screen.getByTestId("workflow-graph-node-terminal"));
    expect(onNodeInspect).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("workflow-graph-node-join"));
    expect(onNodeInspect).toHaveBeenCalledWith("join");
    fireEvent.click(screen.getByTestId("workflow-graph-node-agent"));
    expect(onNodeInspect).toHaveBeenCalledWith("agent");
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

    fireEvent.pointerMove(screen.getByTestId("workflow-graph-node-start"), { pointerType: "mouse" });
    await waitFor(() => {
      expect(screen.getByTestId("workflow-node-metadata-tooltip")).toHaveClass("w-[420px]");
    });
    expect(screen.getByTestId("workflow-node-metadata-tooltip")).toHaveClass(
      "max-w-[calc(100vw-var(--space-4)*2)]",
    );

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
      hasError,
      key: nodeKey,
      kind,
      label,
      role: kind === "agent" ? "coder" : "",
    },
    draggable: false,
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
