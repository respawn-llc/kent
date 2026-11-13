import { describe, expect, it } from "bun:test";
import {
  draftDefinitionFromSource,
  workflowDefinitionFromDraft,
  type DraftWorkflowDefinition,
} from "./workflowEditorDraft";
import { edgesForTransition, groupableWorkflowDefinition } from "./workflowEditorGraphMutationFixtures";
import {
  addWorkflowNode,
  addWorkflowNodeToGroup,
  createWorkflowNodeGroupFromNode,
  deleteWorkflowNode,
  deleteWorkflowNodeGroup,
  extractWorkflowNodeFromGroup,
  removeWorkflowNodeFromGroup,
} from "./workflowEditorGraphNodeMutations";

const groupID = "20000000-0000-4000-8000-000000000010";
const joinNodeID = "10000000-0000-4000-8000-000000000010";
const secondBranchID = "10000000-0000-4000-8000-000000000011";
const thirdBranchID = "10000000-0000-4000-8000-000000000012";
const rehomedIncomingTransitionGroupID = "40000000-0000-4000-8000-000000000099";

function parallelDraft(branchCount: 2 | 3, loaded: boolean): DraftWorkflowDefinition {
  let draft = createWorkflowNodeGroupFromNode(draftDefinitionFromSource(groupableWorkflowDefinition), {
    groupID,
    joinNodeID,
    nodeID: "node-source",
  }).draft;
  for (const [index, nodeID] of [secondBranchID, thirdBranchID].slice(0, branchCount - 1).entries()) {
    draft = addWorkflowNode(draft, {
      id: nodeID,
      key: index === 0 ? "a_branch" : "b_branch",
      kind: "agent",
    }).draft;
    const result = addWorkflowNodeToGroup(draft, {
      groupID,
      nodeID,
      inferredTopologyIDs: {
        addedBranchJoinEdgeID: `30000000-0000-4000-8000-00000000001${index.toString()}`,
        addedBranchJoinTransitionGroupID: `40000000-0000-4000-8000-00000000001${index.toString()}`,
        existingBranchJoinEdgeID: `30000000-0000-4000-8000-00000000002${index.toString()}`,
        existingBranchJoinTransitionGroupID: `40000000-0000-4000-8000-00000000002${index.toString()}`,
        fanoutEdgeID: `30000000-0000-4000-8000-00000000003${index.toString()}`,
      },
    });
    expect(result.warnings).toEqual([]);
    draft = result.draft;
  }
  if (!loaded) {
    return draft;
  }
  return draftDefinitionFromSource(workflowDefinitionFromDraft(draft));
}

function expectDissolved(
  draft: DraftWorkflowDefinition,
  preservedBranchID: string,
  fanoutTargets: readonly string[] = [preservedBranchID],
) {
  expect(draft.nodeGroups).toEqual([]);
  expect(draft.nodes.some((node) => node.id === joinNodeID)).toBe(false);
  expect(draft.nodes.every((node) => node.groupID === null)).toBe(true);
  expect(edgesForTransition(draft, "group-start").map((edge) => edge.targetNodeID)).toEqual([
    ...fanoutTargets,
  ]);
  const downstream = draft.transitionGroups.find((group) =>
    edgesForTransition(draft, group.id).some((edge) => edge.id === "edge-source-agent"),
  );
  expect(downstream?.sourceNodeID).toBe(preservedBranchID);
  expect(draft.edges.some((edge) => edge.targetNodeID === joinNodeID)).toBe(false);
}

describe("Node Group dissolution", () => {
  for (const loaded of [false, true]) {
    describe(loaded ? "loaded group" : "locally expanded group", () => {
      for (const deletion of ["group", "join"] as const) {
        it(`preserves the first Node rather than the first key when deleting the ${deletion}`, () => {
          const draft = parallelDraft(3, loaded);
          const result =
            deletion === "group"
              ? deleteWorkflowNodeGroup(draft, groupID)
              : deleteWorkflowNode(draft, joinNodeID);
          expect(result.warnings).toEqual([]);
          expectDissolved(result.draft, "node-source");
          expect(result.draft.nodes.some((node) => node.id === secondBranchID)).toBe(true);
          expect(result.draft.nodes.some((node) => node.id === thirdBranchID)).toBe(true);
        });
      }

      for (const operation of ["remove", "extract", "delete"] as const) {
        for (const branchCount of [2, 3] as const) {
          it(`${operation}s the first branch from ${branchCount.toString()} branches and retains the remaining route`, () => {
            const draft = parallelDraft(branchCount, loaded);
            const result =
              operation === "remove"
                ? removeWorkflowNodeFromGroup(draft, "node-source")
                : operation === "delete"
                  ? deleteWorkflowNode(draft, "node-source")
                  : extractWorkflowNodeFromGroup(draft, {
                      nodeID: "node-source",
                      rehomedIncomingTransitionGroupID,
                    });
            expect(result.warnings).toEqual([]);
            if (operation === "delete") {
              expect(result.draft.nodes.some((node) => node.id === "node-source")).toBe(false);
            } else {
              expect(result.draft.nodes.find((node) => node.id === "node-source")?.groupID).toBeNull();
            }
            if (operation === "extract") {
              expect(
                edgesForTransition(result.draft, rehomedIncomingTransitionGroupID).map(
                  (edge) => edge.targetNodeID,
                ),
              ).toEqual(["node-source"]);
            }
            if (branchCount === 3) {
              expect(result.draft.nodes.find((node) => node.id === secondBranchID)?.groupID).toBe(groupID);
              expect(result.draft.nodes.find((node) => node.id === thirdBranchID)?.groupID).toBe(groupID);
              expect(result.draft.nodes.some((node) => node.id === joinNodeID)).toBe(true);
            }
            const dissolved = branchCount === 3 ? deleteWorkflowNodeGroup(result.draft, groupID) : result;
            expectDissolved(
              dissolved.draft,
              secondBranchID,
              operation === "remove" && branchCount === 3
                ? ["node-source", secondBranchID]
                : [secondBranchID],
            );
          });
        }
      }
    });
  }
});
