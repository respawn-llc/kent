import type { WorkflowGraphEdge, WorkflowGraphNode } from "./workflowGraphLayout";
import { workflowGraphRenderEdges, workflowGraphRenderNodes } from "./workflowGraphRenderLayers";
import { workflowGraphLayerClassNames } from "./workflowGraphZOrder";

describe("workflow graph render layers", () => {
  it("assigns explicit z-order layer classes without changing visual styles", () => {
    const [groupNode, workflowNode] = workflowGraphRenderNodes([
      workflowGroupGraphNode("group"),
      workflowNodeGraphNode("agent"),
    ]);
    const [edge] = workflowGraphRenderEdges([workflowGraphEdge()]);

    expect(renderLayerClassName(groupNode)).toContain(workflowGraphLayerClassNames.group);
    expect(renderLayerClassName(workflowNode)).toContain(workflowGraphLayerClassNames.node);
    expect(renderLayerClassName(edge)).toContain(workflowGraphLayerClassNames.edge);
  });
});

function renderLayerClassName(value: unknown): string {
  if (!isRecord(value)) {
    return "";
  }
  const className = value.className;
  return typeof className === "string" ? className : "";
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null;
}

function workflowGroupGraphNode(id: string): WorkflowGraphNode {
  return {
    data: {
      empty: true,
      entityID: id,
      entityKind: "group",
      hasError: false,
      kind: "group",
      label: "Group",
    },
    id,
    position: { x: 0, y: 0 },
    type: "workflowGroup",
  };
}

function workflowNodeGraphNode(id: string): WorkflowGraphNode {
  return {
    data: {
      entityID: id,
      entityKind: "node",
      groupID: "",
      hasError: false,
      key: id,
      kind: "agent",
      label: "Agent",
      role: "coder",
    },
    id,
    position: { x: 0, y: 0 },
    type: "workflowNode",
  };
}

function workflowGraphEdge(): WorkflowGraphEdge {
  return {
    data: {
      contextMode: "compact_and_continue_session",
      entityID: "edge-1",
      entityKind: "edge",
      hasError: false,
      label: "Review",
      routePoints: [],
      transitionGroupID: "tg-1",
    },
    id: "edge-1",
    source: "agent",
    target: "terminal",
    type: "workflow",
  };
}
