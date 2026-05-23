import { describe, expect, it } from "vitest";

import type { WorkflowDefinition, WorkflowValidation } from "../../api";
import type { WorkflowGraphEdge, WorkflowGraphNode } from "./workflowGraphLayout";
import { layoutWorkflowGraph } from "./workflowGraphLayout";

describe("layoutWorkflowGraph", () => {
  it("builds grouped workflow graph nodes and labeled edges", async () => {
    const graph = await layoutWorkflowGraph(groupedWorkflow, emptyValidation);

    expect(nodeByID(graph.nodes, "workflow-group-group-1")?.type).toBe("workflowGroup");
    expect(nodeByID(graph.nodes, "node-1")).toMatchObject({
      parentId: "workflow-group-group-1",
      type: "workflowNode",
    });
    expect(nodeByID(graph.nodes, "done")?.type).toBe("workflowNode");
    expect(edgeByID(graph.edges, "edge-1")).toMatchObject({
      source: "node-1",
      target: "done",
      data: { label: "Done", hasError: false },
      markerEnd: { type: "arrowclosed" },
    });
  });

  it("lays out grouped cross-boundary edges as node-to-node edges", async () => {
    const graph = await layoutWorkflowGraph(crossBoundaryWorkflow, emptyValidation);

    expect(nodeByID(graph.nodes, "node-source")).toMatchObject({
      parentId: "workflow-group-group-source",
      type: "workflowNode",
    });
    expect(nodeByID(graph.nodes, "node-target")).toMatchObject({
      parentId: "workflow-group-group-target",
      type: "workflowNode",
    });
    expect(edgeByID(graph.edges, "edge-cross")).toMatchObject({
      source: "node-source",
      target: "node-target",
      type: "workflow",
    });
    expect(edgeByID(graph.edges, "edge-exit")).toMatchObject({
      source: "node-target",
      target: "done",
      type: "workflow",
    });
  });

  it("marks validation errors and disambiguates fan-out labels", async () => {
    const graph = await layoutWorkflowGraph(fanoutWorkflow, {
      valid: false,
      errors: [
        {
          code: "workflow.validation.invalid",
          message: "Invalid",
          workflowID: "workflow-1",
          nodeID: "node-1",
          transitionGroupID: "tg-split",
          edgeID: "edge-a",
          relatedIDs: ["node-a"],
          blocksContext: true,
        },
      ],
    });

    expect(graph.nodes.find((node) => node.id === "node-1")?.data.hasError).toBe(true);
    expect(graph.nodes.find((node) => node.id === "node-a")?.data.hasError).toBe(true);
    expect(edgeByID(graph.edges, "edge-a")?.data).toMatchObject({ label: "Split / a", hasError: true });
    expect(edgeByID(graph.edges, "edge-b")?.data).toMatchObject({ label: "Split / b", hasError: true });
  });

  it("collapses join nodes into direct visual merge edges", async () => {
    const graph = await layoutWorkflowGraph(joinWorkflow, emptyValidation);

    expect(nodeByID(graph.nodes, "join")).toBeUndefined();
    expect(edgeByID(graph.edges, "edge-join-synth")).toBeUndefined();
    expect(edgeByID(graph.edges, "edge-join-a:edge-join-synth")).toMatchObject({
      source: "node-a",
      target: "synth",
      type: "workflow",
    });
    expect(edgeByID(graph.edges, "edge-join-b:edge-join-synth")).toMatchObject({
      source: "node-b",
      target: "synth",
      type: "workflow",
    });
  });

  it("collapses chained hidden join hops without targeting missing nodes", async () => {
    const graph = await layoutWorkflowGraph(joinChainWorkflow, emptyValidation);

    expect(nodeByID(graph.nodes, "join-a")).toBeUndefined();
    expect(nodeByID(graph.nodes, "join-b")).toBeUndefined();
    expect(graph.edges.map((edge) => edge.target)).toEqual(["synth"]);
    expect(edgeByID(graph.edges, "edge-to-join-a:edge-join-a-join-b:edge-join-b-synth")).toMatchObject({
      source: "node-a",
      target: "synth",
      type: "workflow",
    });
  });
});

function nodeByID(nodes: readonly WorkflowGraphNode[], id: string): WorkflowGraphNode | undefined {
  return nodes.find((node) => node.id === id);
}

function edgeByID(edges: readonly WorkflowGraphEdge[], id: string): WorkflowGraphEdge | undefined {
  return edges.find((edge) => edge.id === id);
}

const emptyValidation: WorkflowValidation = { valid: true, errors: [] };

const groupedWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", graphRevision: 1 },
  nodeGroups: [
    {
      id: "group-1",
      workflowID: "workflow-1",
      key: "core",
      name: "Core",
      sortOrder: 1,
      nodeIDs: ["node-1"],
    },
  ],
  nodes: [
    workflowNode("node-1", "Implement", "agent", "group-1"),
    workflowNode("done", "Done", "terminal", ""),
  ],
  transitionGroups: [
    {
      id: "tg-1",
      workflowID: "workflow-1",
      sourceNodeID: "node-1",
      transitionID: "done",
      name: "Done",
    },
  ],
  edges: [
    {
      id: "edge-1",
      workflowID: "workflow-1",
      transitionGroupID: "tg-1",
      key: "done",
      targetNodeID: "done",
      requiresApproval: false,
      contextMode: "new_session",
      inputBindings: [],
      outputRequirements: [],
    },
  ],
};

const fanoutWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", graphRevision: 1 },
  nodeGroups: [],
  nodes: [
    workflowNode("node-1", "Plan", "agent", ""),
    workflowNode("node-a", "A", "agent", ""),
    workflowNode("node-b", "B", "agent", ""),
  ],
  transitionGroups: [
    {
      id: "tg-split",
      workflowID: "workflow-1",
      sourceNodeID: "node-1",
      transitionID: "split",
      name: "Split",
    },
  ],
  edges: [
    workflowEdge("edge-a", "tg-split", "a", "node-a"),
    workflowEdge("edge-b", "tg-split", "b", "node-b"),
  ],
};

const crossBoundaryWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", graphRevision: 1 },
  nodeGroups: [
    {
      id: "group-source",
      workflowID: "workflow-1",
      key: "source",
      name: "Source",
      sortOrder: 1,
      nodeIDs: ["node-source"],
    },
    {
      id: "group-target",
      workflowID: "workflow-1",
      key: "target",
      name: "Target",
      sortOrder: 2,
      nodeIDs: ["node-target"],
    },
  ],
  nodes: [
    workflowNode("node-source", "Source", "agent", "group-source"),
    workflowNode("node-target", "Target", "agent", "group-target"),
    workflowNode("done", "Done", "terminal", ""),
  ],
  transitionGroups: [
    {
      id: "tg-cross",
      workflowID: "workflow-1",
      sourceNodeID: "node-source",
      transitionID: "cross",
      name: "Cross",
    },
    {
      id: "tg-exit",
      workflowID: "workflow-1",
      sourceNodeID: "node-target",
      transitionID: "exit",
      name: "Exit",
    },
  ],
  edges: [
    workflowEdge("edge-cross", "tg-cross", "cross", "node-target"),
    workflowEdge("edge-exit", "tg-exit", "exit", "done"),
  ],
};

const joinWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", graphRevision: 1 },
  nodeGroups: [],
  nodes: [
    workflowNode("node-a", "A", "agent", ""),
    workflowNode("node-b", "B", "agent", ""),
    workflowNode("join", "Join", "join", ""),
    workflowNode("synth", "Synthesize", "agent", ""),
  ],
  transitionGroups: [
    {
      id: "tg-join-a",
      workflowID: "workflow-1",
      sourceNodeID: "node-a",
      transitionID: "join",
      name: "Join",
    },
    {
      id: "tg-join-b",
      workflowID: "workflow-1",
      sourceNodeID: "node-b",
      transitionID: "join",
      name: "Join",
    },
    {
      id: "tg-join-synth",
      workflowID: "workflow-1",
      sourceNodeID: "join",
      transitionID: "done",
      name: "Done",
    },
  ],
  edges: [
    workflowEdge("edge-join-a", "tg-join-a", "join_a", "join"),
    workflowEdge("edge-join-b", "tg-join-b", "join_b", "join"),
    workflowEdge("edge-join-synth", "tg-join-synth", "synth", "synth"),
  ],
};

const joinChainWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", graphRevision: 1 },
  nodeGroups: [],
  nodes: [
    workflowNode("node-a", "A", "agent", ""),
    workflowNode("join-a", "Join A", "join", ""),
    workflowNode("join-b", "Join B", "join", ""),
    workflowNode("synth", "Synthesize", "agent", ""),
  ],
  transitionGroups: [
    {
      id: "tg-to-join-a",
      workflowID: "workflow-1",
      sourceNodeID: "node-a",
      transitionID: "join",
      name: "Join",
    },
    {
      id: "tg-join-a-join-b",
      workflowID: "workflow-1",
      sourceNodeID: "join-a",
      transitionID: "join",
      name: "Join",
    },
    {
      id: "tg-join-b-synth",
      workflowID: "workflow-1",
      sourceNodeID: "join-b",
      transitionID: "done",
      name: "Done",
    },
  ],
  edges: [
    workflowEdge("edge-to-join-a", "tg-to-join-a", "join_a", "join-a"),
    workflowEdge("edge-join-a-join-b", "tg-join-a-join-b", "join_b", "join-b"),
    workflowEdge("edge-join-b-synth", "tg-join-b-synth", "synth", "synth"),
  ],
};

function workflowNode(id: string, name: string, kind: string, groupID: string) {
  return {
    id,
    workflowID: "workflow-1",
    key: id,
    kind,
    name,
    groupID,
    groupKey: groupID.length > 0 ? "core" : "",
    subagentRole: kind === "agent" ? "coder" : "",
    promptTemplate: "",
    outputFields: [],
  };
}

function workflowEdge(id: string, transitionGroupID: string, key: string, targetNodeID: string) {
  return {
    id,
    workflowID: "workflow-1",
    transitionGroupID,
    key,
    targetNodeID,
    requiresApproval: false,
    contextMode: "new_session",
    inputBindings: [],
    outputRequirements: [],
  };
}
