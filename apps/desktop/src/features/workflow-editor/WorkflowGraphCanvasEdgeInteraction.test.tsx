import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import { initializeI18n } from "../../i18n/setup";
import { WorkflowGraphCanvas } from "./WorkflowGraphCanvas";
import type { WorkflowGraphEdge, WorkflowGraphNode } from "./workflowGraphLayout";

void initializeI18n();

type WorkflowGraphEdgeData = NonNullable<WorkflowGraphEdge["data"]>;

describe("WorkflowGraphCanvas edge interactions", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
  });

  it("keeps node and handle inspection available with a crossing edge route in the canvas graph", () => {
    const onEdgeInspect = vi.fn();
    const onNodeInspect = vi.fn();
    render(
      <WorkflowGraphCanvas
        graph={{
          edges: [
            workflowGraphEdge({
              id: "edge-crossing-agent",
              routePoints: [
                { x: -40, y: 46 },
                { x: 260, y: 46 },
              ],
              source: "start",
              target: "terminal",
            }),
          ],
          nodes: [
            workflowGraphNode({ id: "start", kind: "start", label: "Backlog", x: -280 }),
            workflowGraphNode({ id: "agent", kind: "agent", label: "Agent", x: 0 }),
            workflowGraphNode({ id: "terminal", kind: "terminal", label: "Done", x: 320 }),
          ],
        }}
        onEdgeInspect={onEdgeInspect}
        onGroupInspect={() => undefined}
        onNodeInspect={onNodeInspect}
        onWorkflowInspect={() => undefined}
      />,
    );

    const agent = screen.getByTestId("workflow-graph-node-agent");
    fireEvent.click(agent);
    expect(onNodeInspect).toHaveBeenCalledExactlyOnceWith("agent");
    expect(onEdgeInspect).not.toHaveBeenCalled();

    fireEvent.click(within(agent).getByTestId("workflow-node-source-handle"));
    expect(onNodeInspect).toHaveBeenCalledTimes(2);
    expect(onNodeInspect).toHaveBeenLastCalledWith("agent");
    expect(onEdgeInspect).not.toHaveBeenCalled();
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
  kind,
  label,
  x,
}: Readonly<{
  id: string;
  kind: string;
  label: string;
  x: number;
}>): WorkflowGraphNode {
  return {
    data: {
      entityID: id,
      entityKind: "node",
      groupID: "",
      hasError: false,
      key: id,
      kind,
      label,
      role: kind === "agent" ? "coder" : "",
    },
    draggable: kind === "agent",
    id,
    position: { x, y: 0 },
    style: { height: 92, width: 220 },
    type: "workflowNode",
  };
}

function workflowGraphEdge({
  id,
  routePoints,
  source,
  target,
}: Readonly<{
  id: string;
  routePoints: WorkflowGraphEdgeData["routePoints"];
  source: string;
  target: string;
}>): WorkflowGraphEdge {
  return {
    data: {
      contextMode: "new_session",
      entityID: id,
      entityKind: "edge",
      hasError: false,
      label: "",
      routePoints,
      transitionGroupID: `transition-group-${id}`,
    },
    id,
    source,
    target,
    type: "workflow",
  };
}
