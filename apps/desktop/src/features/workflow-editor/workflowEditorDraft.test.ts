import { describe, expect, it } from "vitest";

import type { WorkflowDefinition } from "../../api";
import {
  initializeWorkflowEditorDraft,
  workflowDefinitionFromDraft,
  workflowEditorDirtyState,
  workflowEditorDraftGraph,
  workflowEditorDraftReducer,
} from "./workflowEditorDraft";

describe("workflowEditorDraft", () => {
  it("tracks metadata and agent node edits separately", () => {
    const initial = initializeWorkflowEditorDraft(workflowDefinition);
    expect(workflowEditorDirtyState(initial)).toEqual({
      dirty: false,
      graphDirty: false,
      metadataDirty: false,
    });

    const metadata = workflowEditorDraftReducer(initial, {
      description: "Updated description",
      name: "Updated workflow",
      type: "editWorkflowMetadata",
    });
    expect(workflowEditorDirtyState(metadata)).toEqual({
      dirty: true,
      graphDirty: false,
      metadataDirty: true,
    });

    const graph = workflowEditorDraftReducer(initial, {
      nodeID: "node-agent",
      patch: { name: "Edited agent" },
      type: "editAgentNode",
    });
    expect(workflowEditorDirtyState(graph)).toEqual({ dirty: true, graphDirty: true, metadataDirty: false });
    expect(workflowDefinitionFromDraft(graph.draft).nodes[0]?.name).toBe("Edited agent");
  });

  it("serializes output field row ids away and supports reorder", () => {
    const added = workflowEditorDraftReducer(initializeWorkflowEditorDraft(workflowDefinition), {
      nodeID: "node-agent",
      type: "addOutputField",
    });
    const rowID = added.draft.nodes[0]?.outputFields[1]?.rowID ?? "";
    const updated = workflowEditorDraftReducer(added, {
      nodeID: "node-agent",
      patch: { description: "Details", name: "details" },
      rowID,
      type: "updateOutputField",
    });
    const moved = workflowEditorDraftReducer(updated, {
      direction: -1,
      nodeID: "node-agent",
      rowID,
      type: "moveOutputField",
    });

    const graph = workflowEditorDraftGraph(moved);
    expect(graph.nodes[0]?.outputFields).toEqual([
      { description: "Details", name: "details" },
      { description: "Summary", name: "summary" },
    ]);
  });

  it("cascades selected-node context source references only when the old key is unique", () => {
    const changed = workflowEditorDraftReducer(initializeWorkflowEditorDraft(workflowDefinition), {
      nodeID: "node-agent",
      patch: { key: "implementation" },
      type: "editAgentNode",
    });
    expect(workflowDefinitionFromDraft(changed.draft).edges[0]?.contextSource.nodeKey).toBe("implementation");

    const firstNode = workflowDefinition.nodes[0];
    if (firstNode === undefined) {
      throw new Error("Expected workflow fixture to include an agent node.");
    }
    const duplicateSource = {
      ...workflowDefinition,
      nodes: [...workflowDefinition.nodes, { ...firstNode, id: "node-agent-2" }],
    };
    const duplicateChanged = workflowEditorDraftReducer(initializeWorkflowEditorDraft(duplicateSource), {
      nodeID: "node-agent",
      patch: { key: "implementation" },
      type: "editAgentNode",
    });
    expect(workflowDefinitionFromDraft(duplicateChanged.draft).edges[0]?.contextSource.nodeKey).toBe(
      "implement",
    );
  });

  it("acknowledges a dirty remote conflict until another remote revision arrives", () => {
    const dirty = workflowEditorDraftReducer(initializeWorkflowEditorDraft(workflowDefinition), {
      description: "Local",
      name: "Workflow",
      type: "editWorkflowMetadata",
    });
    const conflict = workflowEditorDraftReducer(dirty, {
      source: withVersion(workflowDefinition, 2),
      type: "conflict",
    });
    const kept = workflowEditorDraftReducer(conflict, { type: "keepEditing" });

    expect(kept.conflict).toBeNull();
    expect(kept.acknowledgedConflictVersion).toBe(2);
  });
});

function withVersion(source: WorkflowDefinition, version: number): WorkflowDefinition {
  return { ...source, workflow: { ...source.workflow, version } };
}

const workflowDefinition: WorkflowDefinition = {
  workflow: {
    description: "",
    version: 1,
    id: "workflow-1",
    name: "Workflow",
  },
  nodeGroups: [],
  nodes: [
    {
      groupID: "",
      groupKey: "",
      id: "node-agent",
      key: "implement",
      kind: "agent",
      name: "Implement",
      outputFields: [{ description: "Summary", name: "summary" }],
      promptTemplate: "Do work.",
      subagentRole: "coder",
      workflowID: "workflow-1",
    },
  ],
  transitionGroups: [
    {
      id: "group-1",
      name: "Done",
      sourceNodeID: "node-agent",
      transitionID: "done",
      workflowID: "workflow-1",
    },
  ],
  edges: [
    {
      contextMode: "continue_session",
      contextSource: { kind: "selected_node", nodeKey: "implement" },
      id: "edge-1",
      inputBindings: [],
      key: "done",
      outputRequirements: [],
      requiresApproval: false,
      targetNodeID: "node-agent",
      transitionGroupID: "group-1",
      workflowID: "workflow-1",
    },
  ],
};
