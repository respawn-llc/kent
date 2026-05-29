/* eslint-disable max-lines -- Graph mutation scenarios share compact fixtures and regression setup. */
import { describe, expect, it } from "vitest";

import { draftDefinitionFromSource } from "./workflowEditorDraft";
import {
  edgesForTransition,
  groupableWorkflowDefinition,
  joinWorkflowDefinition,
  workflowDefinition,
} from "./workflowEditorGraphMutationFixtures";
import {
  addWorkflowNode,
  addWorkflowNodeToGroup,
  connectWorkflowNodes,
  createWorkflowNodeGroupFromNode,
  deleteWorkflowEdge,
  deleteWorkflowNode,
  deleteWorkflowNodeGroup,
  editWorkflowEdgeRoute,
  removeWorkflowNodeFromGroup,
} from "./workflowEditorGraphMutations";

describe("workflowEditorGraphMutations", () => {
  it("adds agent and terminal nodes", () => {
    const agent = addWorkflowNode(draftDefinitionFromSource(workflowDefinition), {
      id: "workflow-node-agent",
      kind: "agent",
      name: "Review",
    });
    const terminal = addWorkflowNode(agent.draft, {
      id: "workflow-node-terminal",
      kind: "terminal",
      name: "Archived",
    });

    expect(agent.draft.nodes.find((node) => node.id === "workflow-node-agent")).toMatchObject({
      key: "review",
      kind: "agent",
      subagentRole: "default",
    });
    expect(terminal.draft.nodes.find((node) => node.id === "workflow-node-terminal")).toMatchObject({
      key: "archived",
      kind: "terminal",
      subagentRole: "",
    });
  });

  it("connects nodes with a new transition group and edge", () => {
    const connected = connectWorkflowNodes(draftDefinitionFromSource(workflowDefinition), {
      edgeID: "workflow-edge-review",
      sourceNodeID: "node-agent",
      targetNodeID: "node-done",
      transitionGroupID: "workflow-transition-group-review",
    });

    expect(connected.draft.transitionGroups.at(-1)).toMatchObject({
      id: "workflow-transition-group-review",
      sourceNodeID: "node-agent",
      transitionID: "done_2",
    });
    expect(connected.draft.edges.at(-1)).toMatchObject({
      contextMode: "new_session",
      id: "workflow-edge-review",
      key: "done",
      targetNodeID: "node-done",
      transitionGroupID: "workflow-transition-group-review",
    });
  });

  it("allows draft edges from start while preserving graph shape for validation", () => {
    const connected = connectWorkflowNodes(draftDefinitionFromSource(workflowDefinition), {
      edgeID: "workflow-edge-start-extra",
      sourceNodeID: "node-start",
      targetNodeID: "node-agent",
      transitionGroupID: "workflow-transition-group-start-extra",
    });

    expect(connected.warnings).toEqual([]);
    expect(connected.draft.transitionGroups.at(-1)?.sourceNodeID).toBe("node-start");
  });

  it("blocks draft edges into start nodes", () => {
    const draft = draftDefinitionFromSource(workflowDefinition);

    const connected = connectWorkflowNodes(draft, {
      edgeID: "workflow-edge-into-start",
      sourceNodeID: "node-agent",
      targetNodeID: "node-start",
      transitionGroupID: "workflow-transition-group-into-start",
    });

    expect(connected.warnings).toEqual(["start nodes cannot have incoming edges"]);
    expect(connected.draft).toBe(draft);
  });

  it("deletes final edge and removes its transition group", () => {
    const deleted = deleteWorkflowEdge(draftDefinitionFromSource(workflowDefinition), "edge-done");

    expect(deleted.draft.edges.some((edge) => edge.id === "edge-done")).toBe(false);
    expect(deleted.draft.transitionGroups.some((group) => group.id === "group-done")).toBe(false);
    expect(deleted.summary).toEqual({
      removedEdgeIDs: ["edge-done"],
      removedNodeIDs: [],
      removedTransitionGroupIDs: ["group-done"],
    });
  });

  it("deletes nodes with incident edges, transition groups, join providers, and selected context sources", () => {
    const deleted = deleteWorkflowNode(draftDefinitionFromSource(joinWorkflowDefinition), "node-branch-a");

    expect(deleted.draft.nodes.some((node) => node.id === "node-branch-a")).toBe(false);
    expect(deleted.draft.edges.map((edge) => edge.id)).not.toContain("edge-branch-a-join");
    expect(deleted.draft.nodes.find((node) => node.id === "node-join")?.joinInputProviders).toEqual([
      { inputName: "risk", providerEdgeID: "edge-branch-b-join" },
    ]);
    expect(deleted.draft.edges.find((edge) => edge.id === "edge-join-done")?.contextSource).toEqual({
      kind: "immediate_source",
      nodeKey: "",
    });
  });

  it("deletes grouped branches by dissolving groups that would become invalid", () => {
    const expanded = inferredTwoBranchGroupDraft();

    const deleted = deleteWorkflowNode(expanded, "node-review");

    expect(deleted.draft.nodes.some((node) => node.id === "node-review")).toBe(false);
    expect(deleted.draft.nodes.some((node) => node.id === "node-join")).toBe(false);
    expect(deleted.draft.nodeGroups.some((group) => group.id === "workflow-node-group-parallel")).toBe(false);
    expect(edgesForTransition(deleted.draft, "group-source-agent")).toMatchObject([
      { targetNodeID: "node-agent" },
    ]);
    expect(deleted.draft.transitionGroups.find((group) => group.id === "group-done")).toMatchObject({
      sourceNodeID: "node-agent",
    });
    expect(deleted.summary.removedNodeIDs).toEqual(["node-review", "node-join"]);
  });

  it("deletes grouped joins by dissolving their node group", () => {
    const expanded = inferredTwoBranchGroupDraft();

    const deleted = deleteWorkflowNode(expanded, "node-join");

    expect(deleted.draft.nodes.some((node) => node.id === "node-join")).toBe(false);
    expect(deleted.draft.nodes.find((node) => node.id === "node-agent")).toMatchObject({
      groupID: "",
      groupKey: "",
    });
    expect(deleted.draft.nodes.find((node) => node.id === "node-review")).toMatchObject({
      groupID: "",
      groupKey: "",
    });
    expect(deleted.draft.nodeGroups.some((group) => group.id === "workflow-node-group-parallel")).toBe(false);
    expect(edgesForTransition(deleted.draft, "group-source-agent")).toMatchObject([
      { targetNodeID: "node-agent" },
    ]);
    expect(deleted.draft.transitionGroups.find((group) => group.id === "group-done")).toMatchObject({
      sourceNodeID: "node-agent",
    });
  });

  it("guards start node and last terminal deletion", () => {
    expect(deleteWorkflowNode(draftDefinitionFromSource(workflowDefinition), "node-start").warnings).toEqual([
      "start node cannot be deleted",
    ]);
    expect(deleteWorkflowNode(draftDefinitionFromSource(workflowDefinition), "node-done").warnings).toEqual([
      "last terminal node cannot be deleted",
    ]);
  });

  it("edits edge route facts while keeping derived wiring outside mutation scope", () => {
    const edited = editWorkflowEdgeRoute(draftDefinitionFromSource(workflowDefinition), {
      contextMode: "continue_session",
      contextSource: { kind: "selected_node", nodeKey: "implement" },
      edgeID: "edge-start",
      edgeKey: "implement_again",
      requiresApproval: true,
      transitionID: "start_work",
      transitionName: "Start work",
    });

    expect(edited.draft.edges.find((edge) => edge.id === "edge-start")).toMatchObject({
      contextMode: "continue_session",
      contextSource: { kind: "selected_node", nodeKey: "implement" },
      key: "implement_again",
      requiresApproval: true,
      targetNodeID: "node-agent",
    });
    expect(edited.draft.transitionGroups.find((group) => group.id === "group-start")).toMatchObject({
      name: "Start work",
      transitionID: "start_work",
    });
  });

  it("creates node groups with a join and updates membership", () => {
    const created = createWorkflowNodeGroupFromNode(draftDefinitionFromSource(joinWorkflowDefinition), {
      groupID: "workflow-node-group-parallel",
      joinNodeID: "workflow-node-join-parallel",
      nodeID: "node-branch-a",
    });
    const expanded = addWorkflowNodeToGroup(created.draft, {
      groupID: "workflow-node-group-parallel",
      nodeID: "node-branch-b",
    });
    const removed = removeWorkflowNodeFromGroup(expanded.draft, "node-branch-b");

    expect(created.draft.nodeGroups.at(-1)).toMatchObject({
      id: "workflow-node-group-parallel",
      key: "branch_a_parallel",
      nodeIDs: ["node-branch-a", "workflow-node-join-parallel"],
    });
    expect(created.draft.nodes.find((node) => node.id === "workflow-node-join-parallel")).toMatchObject({
      groupID: "workflow-node-group-parallel",
      kind: "join",
    });
    expect(expanded.draft.nodeGroups.find((group) => group.id === "workflow-node-group-parallel")?.nodeIDs).toEqual([
      "node-branch-a",
      "node-branch-b",
      "workflow-node-join-parallel",
    ]);
    expect(removed.draft.nodes.find((node) => node.id === "node-branch-a")?.groupID).toBe("");
    expect(removed.draft.nodes.find((node) => node.id === "node-branch-b")?.groupID).toBe("");
    expect(removed.draft.nodeGroups.some((group) => group.id === "workflow-node-group-parallel")).toBe(false);
  });

  it("safely infers v1 node group fan-out and join topology when adding an unconnected branch", () => {
    const withBranch = addWorkflowNode(draftDefinitionFromSource(groupableWorkflowDefinition), {
      id: "node-review",
      kind: "agent",
      name: "Review",
    });
    const created = createWorkflowNodeGroupFromNode(withBranch.draft, {
      groupID: "workflow-node-group-parallel",
      joinNodeID: "node-join",
      nodeID: "node-agent",
    });
    const expanded = addWorkflowNodeToGroup(created.draft, {
      groupID: "workflow-node-group-parallel",
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: "edge-review-join",
        addedBranchJoinTransitionGroupID: "group-review-join",
        existingBranchJoinEdgeID: "edge-implement-join",
        existingBranchJoinTransitionGroupID: "group-implement-join",
        fanoutEdgeID: "edge-start-review",
      },
      nodeID: "node-review",
    });

    expect(edgesForTransition(expanded.draft, "group-source-agent").map((edge) => edge.targetNodeID).sort()).toEqual([
      "node-agent",
      "node-review",
    ]);
    expect(expanded.draft.transitionGroups.find((group) => group.id === "group-done")?.sourceNodeID).toBe("node-join");
    expect(expanded.draft.edges.find((edge) => edge.id === "edge-implement-join")).toMatchObject({
      targetNodeID: "node-join",
      transitionGroupID: "group-implement-join",
    });
    expect(expanded.draft.edges.find((edge) => edge.id === "edge-review-join")).toMatchObject({
      targetNodeID: "node-join",
      transitionGroupID: "group-review-join",
    });
    expect(expanded.draft.transitionGroups.find((group) => group.id === "group-implement-join")).toMatchObject({
      sourceNodeID: "node-agent",
      transitionID: "implement_parallel_join",
    });
    expect(expanded.draft.transitionGroups.find((group) => group.id === "group-review-join")).toMatchObject({
      sourceNodeID: "node-review",
      transitionID: "implement_parallel_join",
    });
  });

  it("blocks adding a branch when node group topology cannot be inferred safely", () => {
    const withBranch = addWorkflowNode(draftDefinitionFromSource(workflowDefinition), {
      id: "node-review",
      kind: "agent",
      name: "Review",
    });
    const created = createWorkflowNodeGroupFromNode(withBranch.draft, {
      groupID: "workflow-node-group-parallel",
      joinNodeID: "node-join",
      nodeID: "node-agent",
    });

    const expanded = addWorkflowNodeToGroup(created.draft, {
      groupID: "workflow-node-group-parallel",
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: "edge-review-join",
        addedBranchJoinTransitionGroupID: "group-review-join",
        existingBranchJoinEdgeID: "edge-implement-join",
        existingBranchJoinTransitionGroupID: "group-implement-join",
        fanoutEdgeID: "edge-start-review",
      },
      nodeID: "node-review",
    });

    expect(expanded.warnings).toEqual(["node group topology could not be inferred safely"]);
    expect(expanded.draft).toBe(created.draft);
    expect(expanded.draft.nodes.find((node) => node.id === "node-review")?.groupID).toBe("");
  });

  it("infers v1 node group topology when adding another branch to an existing valid group", () => {
    const withReview = addWorkflowNode(draftDefinitionFromSource(groupableWorkflowDefinition), {
      id: "node-review",
      kind: "agent",
      name: "Review",
    });
    const created = createWorkflowNodeGroupFromNode(withReview.draft, {
      groupID: "workflow-node-group-parallel",
      joinNodeID: "node-join",
      nodeID: "node-agent",
    });
    const withTwoBranches = addWorkflowNodeToGroup(created.draft, {
      groupID: "workflow-node-group-parallel",
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: "edge-review-join",
        addedBranchJoinTransitionGroupID: "group-review-join",
        existingBranchJoinEdgeID: "edge-implement-join",
        existingBranchJoinTransitionGroupID: "group-implement-join",
        fanoutEdgeID: "edge-start-review",
      },
      nodeID: "node-review",
    });
    const withAudit = addWorkflowNode(withTwoBranches.draft, {
      id: "node-audit",
      kind: "agent",
      name: "Audit",
    });

    const withThreeBranches = addWorkflowNodeToGroup(withAudit.draft, {
      groupID: "workflow-node-group-parallel",
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: "edge-audit-join",
        addedBranchJoinTransitionGroupID: "group-audit-join",
        existingBranchJoinEdgeID: "edge-unused-existing-join",
        existingBranchJoinTransitionGroupID: "group-unused-existing-join",
        fanoutEdgeID: "edge-start-audit",
      },
      nodeID: "node-audit",
    });

    expect(edgesForTransition(withThreeBranches.draft, "group-source-agent").map((edge) => edge.targetNodeID).sort()).toEqual([
      "node-agent",
      "node-audit",
      "node-review",
    ]);
    expect(edgesForTransition(withThreeBranches.draft, "group-audit-join")).toMatchObject([
      { id: "edge-audit-join", targetNodeID: "node-join" },
    ]);
    expect(withThreeBranches.draft.transitionGroups.find((group) => group.id === "group-audit-join")).toMatchObject({
      sourceNodeID: "node-audit",
      transitionID: "implement_parallel_join",
    });
    expect(withThreeBranches.draft.edges.some((edge) => edge.id === "edge-unused-existing-join")).toBe(false);
  });

  it("guards join nodes from ungrouping", () => {
    const created = createWorkflowNodeGroupFromNode(draftDefinitionFromSource(workflowDefinition), {
      groupID: "workflow-node-group-parallel",
      joinNodeID: "node-join",
      nodeID: "node-agent",
    });

    const removed = removeWorkflowNodeFromGroup(created.draft, "node-join");

    expect(removed.warnings).toEqual(["node group membership can be changed for agent nodes only"]);
    expect(removed.draft).toBe(created.draft);
  });

  it("deletes node groups by ungrouping branch nodes and cascading owned join rows", () => {
    const created = createWorkflowNodeGroupFromNode(draftDefinitionFromSource(joinWorkflowDefinition), {
      groupID: "workflow-node-group-parallel",
      joinNodeID: "workflow-node-join-parallel",
      nodeID: "node-branch-a",
    });
    const expanded = addWorkflowNodeToGroup(created.draft, {
      groupID: "workflow-node-group-parallel",
      nodeID: "node-branch-b",
    });

    const deleted = deleteWorkflowNodeGroup(expanded.draft, "workflow-node-group-parallel");

    expect(deleted.draft.nodeGroups.some((group) => group.id === "workflow-node-group-parallel")).toBe(false);
    expect(deleted.draft.nodes.find((node) => node.id === "node-branch-a")).toMatchObject({
      groupID: "",
      groupKey: "",
    });
    expect(deleted.draft.nodes.find((node) => node.id === "node-branch-b")).toMatchObject({
      groupID: "",
      groupKey: "",
    });
    expect(deleted.draft.nodes.some((node) => node.id === "workflow-node-join-parallel")).toBe(false);
    expect(deleted.summary.removedNodeIDs).toEqual(["workflow-node-join-parallel"]);
  });

  it("deletes inferred node groups while preserving downstream wiring through one branch", () => {
    const expanded = inferredTwoBranchGroupDraft();

    const deleted = deleteWorkflowNodeGroup(expanded, "workflow-node-group-parallel");

    expect(edgesForTransition(deleted.draft, "group-source-agent")).toMatchObject([
      { targetNodeID: "node-agent" },
    ]);
    expect(deleted.draft.transitionGroups.find((group) => group.id === "group-done")).toMatchObject({
      sourceNodeID: "node-agent",
    });
    expect(deleted.draft.nodes.some((node) => node.id === "node-join")).toBe(false);
    expect(deleted.draft.edges.some((edge) => edge.targetNodeID === "node-join")).toBe(false);
    expect([...deleted.summary.removedEdgeIDs].sort()).toEqual([
      "edge-implement-join",
      "edge-review-join",
      "edge-start-review",
    ]);
  });

  it("dissolves node groups instead of leaving a single remaining branch", () => {
    const withBranch = addWorkflowNode(draftDefinitionFromSource(groupableWorkflowDefinition), {
      id: "node-review",
      kind: "agent",
      name: "Review",
    });
    const created = createWorkflowNodeGroupFromNode(withBranch.draft, {
      groupID: "workflow-node-group-parallel",
      joinNodeID: "node-join",
      nodeID: "node-agent",
    });
    const expanded = addWorkflowNodeToGroup(created.draft, {
      groupID: "workflow-node-group-parallel",
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: "edge-review-join",
        addedBranchJoinTransitionGroupID: "group-review-join",
        existingBranchJoinEdgeID: "edge-implement-join",
        existingBranchJoinTransitionGroupID: "group-implement-join",
        fanoutEdgeID: "edge-start-review",
      },
      nodeID: "node-review",
    });

    const removed = removeWorkflowNodeFromGroup(expanded.draft, "node-review");

    expect(removed.draft.nodeGroups.some((group) => group.id === "workflow-node-group-parallel")).toBe(false);
    expect(removed.draft.nodes.find((node) => node.id === "node-agent")).toMatchObject({
      groupID: "",
      groupKey: "",
    });
    expect(removed.draft.nodes.find((node) => node.id === "node-review")).toMatchObject({
      groupID: "",
      groupKey: "",
    });
    expect(removed.draft.nodes.some((node) => node.id === "node-join")).toBe(false);
    expect(edgesForTransition(removed.draft, "group-source-agent")).toMatchObject([
      { targetNodeID: "node-agent" },
    ]);
    expect(removed.draft.transitionGroups.find((group) => group.id === "group-done")).toMatchObject({
      sourceNodeID: "node-agent",
    });
    expect(removed.summary.removedNodeIDs).toEqual(["node-join"]);
  });
});

function inferredTwoBranchGroupDraft() {
  const withBranch = addWorkflowNode(draftDefinitionFromSource(groupableWorkflowDefinition), {
    id: "node-review",
    kind: "agent",
    name: "Review",
  });
  const created = createWorkflowNodeGroupFromNode(withBranch.draft, {
    groupID: "workflow-node-group-parallel",
    joinNodeID: "node-join",
    nodeID: "node-agent",
  });
  return addWorkflowNodeToGroup(created.draft, {
    groupID: "workflow-node-group-parallel",
    inferredTopologyIDs: {
      addedBranchJoinEdgeID: "edge-review-join",
      addedBranchJoinTransitionGroupID: "group-review-join",
      existingBranchJoinEdgeID: "edge-implement-join",
      existingBranchJoinTransitionGroupID: "group-implement-join",
      fanoutEdgeID: "edge-start-review",
    },
    nodeID: "node-review",
  }).draft;
}
