import type { Node } from "@xyflow/react";

import type { WorkflowGraphEdge, WorkflowGraphNode } from "./workflowGraphLayout";
import { workflowGraphLayerClassNames } from "./workflowGraphZOrder";

export function workflowGraphRenderNodes(nodes: readonly WorkflowGraphNode[]): Node[] {
  return nodes.map((node) => ({
    ...node,
    className: workflowGraphLayerClassName(node.className, workflowGraphNodeLayerClassName(node)),
  }));
}

export function workflowGraphRenderEdges(edges: readonly WorkflowGraphEdge[]): WorkflowGraphEdge[] {
  return edges.map((edge) => ({
    ...edge,
    className: workflowGraphLayerClassName(edge.className, workflowGraphLayerClassNames.edge),
  }));
}

function workflowGraphNodeLayerClassName(node: WorkflowGraphNode): string {
  return node.data.entityKind === "group"
    ? workflowGraphLayerClassNames.group
    : workflowGraphLayerClassNames.node;
}

function workflowGraphLayerClassName(existing: string | undefined, layer: string): string {
  return existing === undefined || existing.length === 0 ? layer : `${existing} ${layer}`;
}
