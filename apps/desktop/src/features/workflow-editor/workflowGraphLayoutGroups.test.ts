import { describe, expect, it } from "vitest";

import { emptyWorkflowDerivedWiring, type WorkflowDefinition, type WorkflowValidation } from "../../api";
import type { WorkflowGraphNode } from "./workflowGraphLayout";
import { layoutWorkflowGraph } from "./workflowGraphLayout";

describe("layoutWorkflowGraph node group bounds", () => {
  it("keeps unrelated nodes outside populated node group bounds", async () => {
    const graph = await layoutWorkflowGraph(threeBranchGroupWorkflow, emptyValidation);
    const group = requiredNodeByID(graph.nodes, "workflow-group-group-1");
    const unrelatedNodes = ["start", "done"].map((id) => requiredNodeByID(graph.nodes, id));

    for (const node of unrelatedNodes) {
      expect(rectsOverlap(group, node)).toBe(false);
    }
  });
});

function requiredNodeByID(nodes: readonly WorkflowGraphNode[], id: string): WorkflowGraphNode {
  const node = nodes.find((item) => item.id === id);
  if (node === undefined) {
    throw new Error(`Node ${id} not found`);
  }
  return node;
}

function rectsOverlap(left: WorkflowGraphNode, right: WorkflowGraphNode): boolean {
  const leftWidth = Number(left.style?.width ?? 0);
  const leftHeight = Number(left.style?.height ?? 0);
  const rightWidth = Number(right.style?.width ?? 0);
  const rightHeight = Number(right.style?.height ?? 0);
  return (
    left.position.x < right.position.x + rightWidth &&
    left.position.x + leftWidth > right.position.x &&
    left.position.y < right.position.y + rightHeight &&
    left.position.y + leftHeight > right.position.y
  );
}

const emptyValidation: WorkflowValidation = { valid: true, errors: [] };

const threeBranchGroupWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Group Proof", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
  nodeGroups: [
    {
      id: "group-1",
      workflowID: "workflow-1",
      key: "parallel",
      name: "Parallel",
      sortOrder: 1,
      nodeIDs: ["node-a", "node-b", "node-c", "join"],
    },
  ],
  nodes: [
    workflowNode("start", "Backlog", "start", ""),
    workflowNode("done", "Done", "terminal", ""),
    workflowNode("node-a", "A", "agent", "group-1"),
    workflowNode("node-b", "B", "agent", "group-1"),
    workflowNode("node-c", "C", "agent", "group-1"),
    workflowNode("join", "Join", "join", "group-1"),
  ],
  transitionGroups: [],
  edges: [],
};

function workflowNode(id: string, name: string, kind: string, groupID: string) {
  return {
    groupID,
    groupKey: groupID.length > 0 ? "parallel" : "",
    id,
    inputFields: [],
    joinInputProviders: [],
    key: id,
    kind,
    name,
    outputFields: [],
    promptTemplate: "",
    subagentRole: kind === "agent" ? "coder" : "",
    workflowID: "workflow-1",
  };
}
