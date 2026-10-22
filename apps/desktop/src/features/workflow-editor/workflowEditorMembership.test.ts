import { describe, expect, it } from "bun:test";

import {
  draftDefinitionFromSource,
  initializeWorkflowEditorDraft,
  workflowDefinitionFromDraft,
  workflowEditorDraftReducer,
  workflowEditorDraftGraph,
} from "./workflowEditorDraft";
import {
  addWorkflowNode,
  addWorkflowNodeToGroup,
  createWorkflowNodeGroupFromNode,
  removeWorkflowNodeFromGroup,
} from "./workflowEditorGraphNodeMutations";
import { edgesForTransition, groupableWorkflowDefinition } from "./workflowEditorGraphMutationFixtures";
import { workflowEditorGraphMutationWarnings } from "./workflowEditorGraphMutationTypes";

describe("workflow editor Node Group membership", () => {
  it("creates an unnamed Group independently of its stable key", () => {
    const grouped = createWorkflowNodeGroupFromNode(draftDefinitionFromSource(groupableWorkflowDefinition), {
      groupID: "20000000-0000-4000-8000-000000000004",
      joinNodeID: "10000000-0000-4000-8000-000000000011",
      nodeID: "node-agent",
    });
    expect(grouped.draft.nodeGroups[0]?.name).toBeNull();
    expect(grouped.draft.nodeGroups[0]?.key.length).toBeGreaterThan(0);
  });
  it("edits and clears a Group name through the draft and save projection", () => {
    const source = {
      ...groupableWorkflowDefinition,
      nodeGroups: [
        {
          id: "group",
          key: "parallel",
          name: "Existing",
          sortOrder: 0,
          workflowID: groupableWorkflowDefinition.workflow.id,
          nodeIDs: [],
        },
      ],
    };
    let state = initializeWorkflowEditorDraft(source);
    expect(workflowEditorDraftGraph(state).nodeGroups[0]?.name).toBe("Existing");
    state = workflowEditorDraftReducer(state, { type: "editNodeGroupName", groupID: "group", name: null });
    expect(workflowEditorDraftGraph(state).nodeGroups[0]?.name).toBeNull();
    state = initializeWorkflowEditorDraft(workflowDefinitionFromDraft(state.draft));
    expect(state.draft.nodeGroups[0]?.name).toBeNull();
    state = workflowEditorDraftReducer(state, {
      type: "editNodeGroupName",
      groupID: "group",
      name: "Renamed",
    });
    expect(workflowEditorDraftGraph(state).nodeGroups[0]?.name).toBe("Renamed");
  });
  it("represents absent membership as null through creation, removal, and Draft projection", () => {
    const source = draftDefinitionFromSource(groupableWorkflowDefinition);
    const addedNodeID = "10000000-0000-4000-8000-000000000005";
    const groupID = "20000000-0000-4000-8000-000000000001";
    const joinNodeID = "10000000-0000-4000-8000-000000000006";

    const added = addWorkflowNode(source, { id: addedNodeID, kind: "agent" });
    expect(added.draft.nodes.find((node) => node.id === addedNodeID)?.groupID).toBeNull();

    const grouped = createWorkflowNodeGroupFromNode(source, {
      groupID,
      joinNodeID,
      nodeID: "node-agent",
    });
    expect(grouped.draft.nodes.find((node) => node.id === "node-agent")?.groupID).toBe(groupID);

    const removed = removeWorkflowNodeFromGroup(grouped.draft, "node-agent");
    expect(removed.draft.nodes.find((node) => node.id === "node-agent")?.groupID).toBeNull();

    const projected = workflowEditorDraftGraph(initializeWorkflowEditorDraft(groupableWorkflowDefinition));
    expect(projected.nodes.every((node) => node.groupID === null)).toBe(true);
  });

  it("infers direct Start fan-out when a disconnected branch joins an existing group", () => {
    const source = draftDefinitionFromSource(groupableWorkflowDefinition);
    const groupID = "20000000-0000-4000-8000-000000000002";
    const joinNodeID = "10000000-0000-4000-8000-000000000007";
    const siblingNodeID = "10000000-0000-4000-8000-000000000008";
    const grouped = createWorkflowNodeGroupFromNode(source, {
      groupID,
      joinNodeID,
      nodeID: "node-source",
    });
    const sibling = addWorkflowNode(grouped.draft, { id: siblingNodeID, kind: "agent" });

    const result = addWorkflowNodeToGroup(sibling.draft, {
      groupID,
      nodeID: siblingNodeID,
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: "30000000-0000-4000-8000-000000000001",
        addedBranchJoinTransitionGroupID: "40000000-0000-4000-8000-000000000001",
        existingBranchJoinEdgeID: "30000000-0000-4000-8000-000000000002",
        existingBranchJoinTransitionGroupID: "40000000-0000-4000-8000-000000000002",
        fanoutEdgeID: "30000000-0000-4000-8000-000000000003",
      },
    });

    expect(result.warnings).toEqual([]);
    const transitionIDs = result.draft.transitionGroups.map((group) => group.transitionID);
    expect(new Set(transitionIDs).size).toBe(transitionIDs.length);
    expect(
      edgesForTransition(result.draft, "group-start")
        .map((edge) => edge.targetNodeID)
        .sort(),
    ).toEqual(["node-source", siblingNodeID].sort());
    expect(
      result.draft.edges.some(
        (edge) =>
          edge.targetNodeID === joinNodeID &&
          result.draft.transitionGroups.some(
            (group) => group.id === edge.transitionGroupID && group.sourceNodeID === "node-source",
          ),
      ),
    ).toBe(true);
    expect(
      result.draft.edges.some(
        (edge) =>
          edge.targetNodeID === joinNodeID &&
          result.draft.transitionGroups.some(
            (group) => group.id === edge.transitionGroupID && group.sourceNodeID === siblingNodeID,
          ),
      ),
    ).toBe(true);
    expect(
      result.draft.transitionGroups.some(
        (group) =>
          group.sourceNodeID === joinNodeID &&
          edgesForTransition(result.draft, group.id).some((edge) => edge.targetNodeID === "node-agent"),
      ),
    ).toBe(true);
  });

  it("keeps membership and warns when Start fan-out wiring is ambiguous", () => {
    const source = draftDefinitionFromSource(groupableWorkflowDefinition);
    const groupID = "20000000-0000-4000-8000-000000000003";
    const joinNodeID = "10000000-0000-4000-8000-000000000009";
    const siblingNodeID = "10000000-0000-4000-8000-000000000010";
    const grouped = createWorkflowNodeGroupFromNode(source, {
      groupID,
      joinNodeID,
      nodeID: "node-source",
    });
    const sibling = addWorkflowNode(grouped.draft, { id: siblingNodeID, kind: "agent" });
    const startEdge = sibling.draft.edges.find((edge) => edge.id === "edge-start");
    if (startEdge === undefined) {
      throw new Error("fixture has no Start edge");
    }
    const ambiguousDraft = {
      ...sibling.draft,
      edges: [
        ...sibling.draft.edges,
        {
          ...startEdge,
          id: "edge-start-ambiguous",
          key: "start_other",
          targetNodeID: "node-agent",
        },
      ],
    };

    const result = addWorkflowNodeToGroup(ambiguousDraft, {
      groupID,
      nodeID: siblingNodeID,
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: "30000000-0000-4000-8000-000000000004",
        addedBranchJoinTransitionGroupID: "40000000-0000-4000-8000-000000000004",
        existingBranchJoinEdgeID: "30000000-0000-4000-8000-000000000005",
        existingBranchJoinTransitionGroupID: "40000000-0000-4000-8000-000000000005",
        fanoutEdgeID: "30000000-0000-4000-8000-000000000006",
      },
    });

    expect(result.warnings).toEqual([workflowEditorGraphMutationWarnings.nodeGroupTopologyInferenceFailed]);
    expect(result.draft.nodes.find((node) => node.id === siblingNodeID)?.groupID).toBe(groupID);
    expect(result.draft.edges).toEqual(ambiguousDraft.edges);
    expect(result.draft.transitionGroups).toEqual(ambiguousDraft.transitionGroups);
  });
});
