import { create } from "@app/server-api-contract";
import {
  GraphDraftSchema,
  GraphMetadataSchema,
  GraphSaveConfirmationSchema,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import type { WorkflowGraphMetadata, WorkflowGraphSaveConfirmation } from "./models";
import type { WorkflowGraphDraft } from "./workflowGraphModels";
import {
  workflowAssigneeSelection,
  workflowCompletionMode,
  workflowContextMode,
  workflowContextSourceKind,
  workflowExecutionTargetMode,
  workflowNodeKind,
  workflowParameterPurpose,
  workflowThinkingSelection,
} from "./workflowProtoValues";

export function workflowGraphDraftPayload(graph: WorkflowGraphDraft) {
  return create(GraphDraftSchema, {
    nodeGroups: graph.nodeGroups.map((group) => ({ id: group.id, key: group.key, displayName: group.name })),
    nodes: graph.nodes.map((node) => ({
      id: node.id,
      key: node.key,
      kind: workflowNodeKind.encode(node.kind),
      displayName: node.name,
      groupId: node.groupID ?? undefined,
      subagentRole: node.subagentRole === "" ? undefined : node.subagentRole,
      completionMode:
        node.completionMode === undefined || node.completionMode === ""
          ? undefined
          : workflowCompletionMode.encode(node.completionMode),
      scriptPath:
        node.scriptPath === undefined || node.scriptPath === null || node.scriptPath.trim().length === 0
          ? undefined
          : node.scriptPath,
      joinInputProviders: node.joinInputProviders.map((provider) => ({
        inputName: provider.inputName,
        providerEdgeId: provider.providerEdgeID,
      })),
    })),
    transitionGroups: graph.transitionGroups.map((group) => ({
      id: group.id,
      sourceNodeId: group.sourceNodeID,
      transitionId: group.transitionID,
      displayName: group.name,
      description: group.description,
    })),
    edges: graph.edges.map((edge) => ({
      id: edge.id,
      transitionGroupId: edge.transitionGroupID,
      key: edge.key,
      targetNodeId: edge.targetNodeID,
      assigneeSelection: workflowAssigneeSelection.encode(edge.assigneeSelection),
      thinkingSelection: workflowThinkingSelection.encode(edge.thinkingSelection),
      requiresApproval: edge.requiresApproval,
      contextMode: workflowContextMode.encode(edge.contextMode),
      contextSource: {
        kind: workflowContextSourceKind.encode(edge.contextSource.kind),
        nodeKey: edge.contextSource.nodeKey === "" ? undefined : edge.contextSource.nodeKey,
      },
      promptTemplate: edge.promptTemplate,
      parameters: edge.parameters.map((parameter) => ({
        key: parameter.key,
        description: parameter.description,
        purpose: workflowParameterPurpose.encode(parameter.purpose),
      })),
    })),
  });
}

export function workflowGraphMetadataPayload(metadata: WorkflowGraphMetadata | undefined) {
  return metadata === undefined
    ? undefined
    : create(GraphMetadataSchema, {
        name: metadata.name,
        description: metadata.description,
        executionTargetPolicy: {
          mode: workflowExecutionTargetMode.encode(metadata.executionTargetPolicy.mode),
          customRef: metadata.executionTargetPolicy.customRef ?? undefined,
        },
      });
}

export function workflowGraphSaveConfirmationPayload(
  confirmation: WorkflowGraphSaveConfirmation | undefined,
) {
  return confirmation === undefined
    ? undefined
    : create(GraphSaveConfirmationSchema, {
        expectedRemovedNodeGroupCount: BigInt(confirmation.expectedRemovedNodeGroupCount),
        expectedRemovedNodeCount: BigInt(confirmation.expectedRemovedNodeCount),
        expectedRemovedTransitionGroupCount: BigInt(confirmation.expectedRemovedTransitionGroupCount),
        expectedRemovedEdgeCount: BigInt(confirmation.expectedRemovedEdgeCount),
        expectedNodeTaskReferenceCount: BigInt(confirmation.expectedNodeTaskReferenceCount),
        expectedEdgeTaskReferenceCount: BigInt(confirmation.expectedEdgeTaskReferenceCount),
      });
}
