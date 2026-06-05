import { describe, expect, it } from "vitest";

import { emptyWorkflowDerivedWiring, type WorkflowDefinition, type WorkflowValidation } from "../../api";
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
          details: { fieldName: "", inputName: "", placeholder: "", providerEdgeID: "" },
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

  it("marks provider edges from structured validation details", async () => {
    const graph = await layoutWorkflowGraph(joinWorkflow, {
      valid: false,
      errors: [
        {
          blocksContext: true,
          code: "workflow.validation.invalid_join_input_provider",
          details: { fieldName: "", inputName: "summary", placeholder: "", providerEdgeID: "edge-join-b" },
          edgeID: "",
          message: "join input provider must reference an incoming edge into the join",
          nodeID: "join",
          relatedIDs: [],
          transitionGroupID: "",
          workflowID: "workflow-1",
        },
      ],
    });

    expect(edgeByID(graph.edges, "edge-join-b")?.data).toMatchObject({ hasError: true });
    expect(edgeByID(graph.edges, "edge-join-a")?.data).toMatchObject({ hasError: false });
  });

  it("renders join nodes as inspectable merge diamonds with raw join edges", async () => {
    const graph = await layoutWorkflowGraph(joinWorkflow, emptyValidation);

    expect(nodeByID(graph.nodes, "join")).toMatchObject({
      type: "workflowJoin",
      data: { entityID: "join", entityKind: "node", kind: "join" },
      style: { width: 56, height: 56 },
    });
    expect(edgeByID(graph.edges, "edge-join-a")).toMatchObject({
      markerEnd: { type: "arrowclosed" },
      source: "node-a",
      target: "join",
      type: "workflow",
    });
    expect(edgeByID(graph.edges, "edge-join-b")).toMatchObject({
      markerEnd: { type: "arrowclosed" },
    });
    expect(edgeByID(graph.edges, "edge-join-synth")).toMatchObject({
      markerEnd: { type: "arrowclosed" },
      source: "join",
      target: "synth",
      type: "workflow",
    });
  });

  it("keeps chained join hops inspectable instead of synthesizing edge chains", async () => {
    const graph = await layoutWorkflowGraph(joinChainWorkflow, emptyValidation);

    expect(nodeByID(graph.nodes, "join-a")).toMatchObject({ type: "workflowJoin" });
    expect(nodeByID(graph.nodes, "join-b")).toMatchObject({ type: "workflowJoin" });
    expect(graph.edges.map((edge) => edge.target)).toEqual(["join-a", "join-b", "synth"]);
    expect(edgeByID(graph.edges, "edge-to-join-a")).toMatchObject({
      source: "node-a",
      target: "join-a",
      type: "workflow",
    });
  });

  it("routes transition endpoints away from the reserved creation handle slot", async () => {
    const graph = await layoutWorkflowGraph(singleTransitionWorkflow, emptyValidation);
    const edge = edgeByID(graph.edges, "edge-source-target");
    const source = nodeByID(graph.nodes, "node-source");
    const target = nodeByID(graph.nodes, "node-target");

    expect(edge?.data?.routePoints.at(0)?.y).not.toBe(nodeCenterY(source));
    expect(edge?.data?.routePoints.at(-1)?.y).not.toBe(nodeCenterY(target));
  });

  it("uses deterministic separate endpoint slots for multiple outgoing transitions", async () => {
    const graph = await layoutWorkflowGraph(twoOutgoingTransitionWorkflow, emptyValidation);
    const source = nodeByID(graph.nodes, "node-source");
    const firstStart = edgeByID(graph.edges, "edge-source-a")?.data?.routePoints.at(0);
    const secondStart = edgeByID(graph.edges, "edge-source-b")?.data?.routePoints.at(0);

    expect(firstStart?.y).not.toBe(nodeCenterY(source));
    expect(secondStart?.y).not.toBe(nodeCenterY(source));
    expect(firstStart?.y).not.toBe(secondStart?.y);
  });

  it("exposes matching React Flow handles for routed transition endpoint slots", async () => {
    const graph = await layoutWorkflowGraph(singleTransitionWorkflow, emptyValidation);
    const edge = edgeByID(graph.edges, "edge-source-target");
    const source = nodeByID(graph.nodes, "node-source");
    const target = nodeByID(graph.nodes, "node-target");
    const sourcePort = endpointPort(source, "source");
    const targetPort = endpointPort(target, "target");

    expect(edge?.sourceHandle).toBe(sourcePort?.id);
    expect(edge?.targetHandle).toBe(targetPort?.id);
    expect(edge?.data?.routePoints.at(0)?.y).toBe(source === undefined ? undefined : source.position.y + sourcePort!.y);
    expect(edge?.data?.routePoints.at(-1)?.y).toBe(target === undefined ? undefined : target.position.y + targetPort!.y);
  });
});

function nodeByID(nodes: readonly WorkflowGraphNode[], id: string): WorkflowGraphNode | undefined {
  return nodes.find((node) => node.id === id);
}

function edgeByID(edges: readonly WorkflowGraphEdge[], id: string): WorkflowGraphEdge | undefined {
  return edges.find((edge) => edge.id === id);
}

function nodeCenterY(node: WorkflowGraphNode | undefined): number | undefined {
  if (node === undefined) {
    return undefined;
  }
  const height = typeof node.style?.height === "number" ? node.style.height : Number(node.style?.height);
  return node.position.y + height / 2;
}

function endpointPort(
  node: WorkflowGraphNode | undefined,
  side: "source" | "target",
): Readonly<{ id: string; side: "source" | "target"; y: number }> | undefined {
  if (node?.data.entityKind !== "node") {
    return undefined;
  }
  const ports = node.data.endpointPorts;
  if (!Array.isArray(ports)) {
    return undefined;
  }
  return ports.find(
    (port): port is Readonly<{ id: string; side: "source" | "target"; y: number }> =>
      typeof port === "object" &&
      port !== null &&
      "id" in port &&
      typeof port.id === "string" &&
      "side" in port &&
      port.side === side &&
      "y" in port &&
      typeof port.y === "number",
  );
}

const emptyValidation: WorkflowValidation = { valid: true, errors: [] };

const groupedWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
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
  transitionGroups: [workflowTransitionGroup("tg-1", "node-1", "done", "Done")],
  edges: [
    {
      id: "edge-1",
      workflowID: "workflow-1",
      transitionGroupID: "tg-1",
      key: "done",
      targetNodeID: "done",
      requiresApproval: false,
      contextMode: "new_session",
      contextSource: { kind: "immediate_source", nodeKey: "" },
      inputBindings: [],
      outputRequirements: [],
      parameters: [],
      promptTemplate: "",
    },
  ],
};

const fanoutWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
  nodeGroups: [],
  nodes: [
    workflowNode("node-1", "Plan", "agent", ""),
    workflowNode("node-a", "A", "agent", ""),
    workflowNode("node-b", "B", "agent", ""),
  ],
  transitionGroups: [workflowTransitionGroup("tg-split", "node-1", "split", "Split")],
  edges: [
    workflowEdge({ id: "edge-a", key: "a", targetNodeID: "node-a", transitionGroupID: "tg-split" }),
    workflowEdge({ id: "edge-b", key: "b", targetNodeID: "node-b", transitionGroupID: "tg-split" }),
  ],
};

const singleTransitionWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
  nodeGroups: [],
  nodes: [
    workflowNode("node-source", "Source", "agent", ""),
    workflowNode("node-target", "Target", "agent", ""),
  ],
  transitionGroups: [workflowTransitionGroup("tg-source-target", "node-source", "target", "Target")],
  edges: [
    workflowEdge({
      id: "edge-source-target",
      key: "target",
      targetNodeID: "node-target",
      transitionGroupID: "tg-source-target",
    }),
  ],
};

const twoOutgoingTransitionWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
  nodeGroups: [],
  nodes: [
    workflowNode("node-source", "Source", "agent", ""),
    workflowNode("node-a", "A", "agent", ""),
    workflowNode("node-b", "B", "agent", ""),
  ],
  transitionGroups: [
    workflowTransitionGroup("tg-source-a", "node-source", "a", "A"),
    workflowTransitionGroup("tg-source-b", "node-source", "b", "B"),
  ],
  edges: [
    workflowEdge({
      id: "edge-source-a",
      key: "a",
      targetNodeID: "node-a",
      transitionGroupID: "tg-source-a",
    }),
    workflowEdge({
      id: "edge-source-b",
      key: "b",
      targetNodeID: "node-b",
      transitionGroupID: "tg-source-b",
    }),
  ],
};

const crossBoundaryWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
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
    workflowTransitionGroup("tg-cross", "node-source", "cross", "Cross"),
    workflowTransitionGroup("tg-exit", "node-target", "exit", "Exit"),
  ],
  edges: [
    workflowEdge({
      id: "edge-cross",
      key: "cross",
      targetNodeID: "node-target",
      transitionGroupID: "tg-cross",
    }),
    workflowEdge({ id: "edge-exit", key: "exit", targetNodeID: "done", transitionGroupID: "tg-exit" }),
  ],
};

const joinWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
  nodeGroups: [],
  nodes: [
    workflowNode("node-a", "A", "agent", ""),
    workflowNode("node-b", "B", "agent", ""),
    workflowNode("join", "Join", "join", ""),
    workflowNode("synth", "Synthesize", "agent", ""),
  ],
  transitionGroups: [
    workflowTransitionGroup("tg-join-a", "node-a", "join", "Join"),
    workflowTransitionGroup("tg-join-b", "node-b", "join", "Join"),
    workflowTransitionGroup("tg-join-synth", "join", "done", "Done"),
  ],
  edges: [
    workflowEdge({
      contextMode: "continue_session",
      id: "edge-join-a",
      key: "join_a",
      targetNodeID: "join",
      transitionGroupID: "tg-join-a",
    }),
    workflowEdge({
      contextMode: "compact_and_continue_session",
      id: "edge-join-b",
      key: "join_b",
      targetNodeID: "join",
      transitionGroupID: "tg-join-b",
    }),
    workflowEdge({
      contextMode: "new_session",
      id: "edge-join-synth",
      key: "synth",
      targetNodeID: "synth",
      transitionGroupID: "tg-join-synth",
    }),
  ],
};

const joinChainWorkflow: WorkflowDefinition = {
  workflow: { id: "workflow-1", name: "Delivery", description: "", version: 1 },
  derivedWiring: emptyWorkflowDerivedWiring,
  nodeGroups: [],
  nodes: [
    workflowNode("node-a", "A", "agent", ""),
    workflowNode("join-a", "Join A", "join", ""),
    workflowNode("join-b", "Join B", "join", ""),
    workflowNode("synth", "Synthesize", "agent", ""),
  ],
  transitionGroups: [
    workflowTransitionGroup("tg-to-join-a", "node-a", "join", "Join"),
    workflowTransitionGroup("tg-join-a-join-b", "join-a", "join", "Join"),
    workflowTransitionGroup("tg-join-b-synth", "join-b", "done", "Done"),
  ],
  edges: [
    workflowEdge({
      id: "edge-to-join-a",
      key: "join_a",
      targetNodeID: "join-a",
      transitionGroupID: "tg-to-join-a",
    }),
    workflowEdge({
      id: "edge-join-a-join-b",
      key: "join_b",
      targetNodeID: "join-b",
      transitionGroupID: "tg-join-a-join-b",
    }),
    workflowEdge({
      id: "edge-join-b-synth",
      key: "synth",
      targetNodeID: "synth",
      transitionGroupID: "tg-join-b-synth",
    }),
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
    inputFields: [],
    joinInputProviders: [],
    outputFields: [],
  };
}

function workflowTransitionGroup(id: string, sourceNodeID: string, transitionID: string, name: string) {
  return { id, workflowID: "workflow-1", sourceNodeID, transitionID, name };
}

function workflowEdge({
  contextMode = "new_session",
  id,
  key,
  targetNodeID,
  transitionGroupID,
}: Readonly<{
  contextMode?: string;
  id: string;
  key: string;
  targetNodeID: string;
  transitionGroupID: string;
}>) {
  return {
    id,
    workflowID: "workflow-1",
    transitionGroupID,
    key,
    targetNodeID,
    requiresApproval: false,
    contextMode,
    contextSource: { kind: "immediate_source", nodeKey: "" },
    inputBindings: [],
    outputRequirements: [],
    parameters: [],
    promptTemplate: "",
  };
}
