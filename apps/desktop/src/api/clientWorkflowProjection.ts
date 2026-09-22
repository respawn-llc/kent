import type * as pb from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import type {
  ProjectWorkflowLink,
  WorkflowDefinition,
  WorkflowDeleteImpact,
  WorkflowDerivedWiring,
  WorkflowGraphEntityReference,
  WorkflowGraphSaveBlocker,
  WorkflowGraphSaveImpact,
  WorkflowGraphSavePreview,
  WorkflowGraphValidationResults,
  WorkflowRecord,
  WorkflowValidation,
  WorkflowValidationError,
  WorkflowValidationErrorDetails,
} from "./models";
import type { WorkflowSelectorApplicability } from "./workflowSelectionModels";
import { ContractError } from "./errors";
import {
  workflowAssigneeSelection,
  workflowCompletionMode,
  workflowContextMode,
  workflowContextSourceKind,
  workflowExecutionTargetMode,
  workflowGraphEntityType,
  workflowNodeKind,
  workflowParameterPurpose,
  workflowSelectorReason,
  workflowThinkingSelection,
  workflowValidationCode,
  workflowValidationMode,
} from "./workflowProtoValues";

export function workflowRecord(value: pb.WorkflowRecord | undefined): WorkflowRecord {
  if (value?.executionTargetPolicy === undefined) {
    throw new ContractError("Workflow record and execution target policy are required.");
  }
  const record: WorkflowRecord = {
    id: value.id,
    name: value.name,
    description: value.description,
    version: Number(value.version),
    executionTargetPolicy: {
      mode: workflowExecutionTargetMode.decode(value.executionTargetPolicy.mode),
      customRef: value.executionTargetPolicy.customRef ?? null,
    },
  };
  return value.projectLink === undefined
    ? record
    : { ...record, projectLink: { isDefault: value.projectLink.default } };
}

export function projectWorkflowLink(value: pb.ProjectWorkflowLink | undefined): ProjectWorkflowLink {
  if (value === undefined) throw new ContractError("Project Workflow link is required.");
  return { id: value.id, projectID: value.projectId, workflowID: value.workflowId, isDefault: value.default };
}

export function workflowDefinition(value: pb.WorkflowDefinition | undefined): WorkflowDefinition {
  if (value === undefined) throw new ContractError("Workflow definition is required.");
  return {
    workflow: workflowRecord(value.workflow),
    nodeGroups: value.nodeGroups.map((group) => ({
      id: group.groupId,
      workflowID: group.workflowId,
      key: group.groupKey,
      name: group.displayName,
      sortOrder: group.sortOrder,
      nodeIDs: [],
    })),
    nodes: value.nodes.map((node) => ({
      id: node.id,
      workflowID: node.workflowId,
      key: node.key,
      kind: workflowNodeKind.decode(node.kind),
      name: node.displayName,
      groupID: node.groupId ?? null,
      groupKey: node.groupKey,
      subagentRole: node.subagentRole ?? "",
      completionMode:
        node.completionMode === undefined ? "" : workflowCompletionMode.decode(node.completionMode),
      scriptPath: node.scriptPath ?? null,
      joinInputProviders: node.joinInputProviders.map((provider) => ({
        inputName: provider.inputName,
        providerEdgeID: provider.providerEdgeId,
      })),
    })),
    transitionGroups: value.transitionGroups.map((group) => ({
      id: group.id,
      workflowID: group.workflowId,
      sourceNodeID: group.sourceNodeId,
      transitionID: group.transitionId,
      name: group.displayName,
      description: group.description,
    })),
    edges: value.edges.map((edge) => {
      if (edge.contextSource === undefined) throw new ContractError("Workflow context source is required.");
      return {
        id: edge.id,
        workflowID: edge.workflowId,
        transitionGroupID: edge.transitionGroupId,
        key: edge.key,
        targetNodeID: edge.targetNodeId,
        assigneeSelection: workflowAssigneeSelection.decode(edge.assigneeSelection),
        thinkingSelection: workflowThinkingSelection.decode(edge.thinkingSelection),
        requiresApproval: edge.requiresApproval,
        contextMode: workflowContextMode.decode(edge.contextMode),
        contextSource: {
          kind: workflowContextSourceKind.decode(edge.contextSource.kind),
          nodeKey: edge.contextSource.nodeKey ?? "",
        },
        promptTemplate: edge.promptTemplate,
        parameters: edge.parameters.map((parameter) => ({
          key: parameter.key,
          description: parameter.description,
          purpose: workflowParameterPurpose.decode(parameter.purpose),
        })),
        inputBindings: edge.inputBindings.map((binding) => ({
          name: binding.name,
          source: binding.source,
          field: binding.field,
        })),
        outputRequirements: edge.outputRequirements.map((requirement) => ({
          fieldName: requirement.fieldName,
        })),
      };
    }),
    derivedWiring: workflowWiring(value.derivedWiring),
  };
}

function workflowValidationError(value: pb.WorkflowValidationError): WorkflowValidationError {
  return {
    code: workflowValidationCode.decode(value.code),
    message: value.message,
    workflowID: value.workflowId ?? null,
    nodeID: value.nodeId ?? null,
    transitionGroupID: value.transitionGroupId ?? null,
    edgeID: value.edgeId ?? null,
    relatedIDs: value.relatedIds,
    blocksContext: value.blocksContext,
    details: workflowValidationDetails(value.details),
  };
}

function workflowValidationDetails(
  details: pb.WorkflowValidationErrorDetails | undefined,
): WorkflowValidationErrorDetails {
  if (details === undefined) {
    return {
      fieldName: "",
      inputName: "",
      placeholder: "",
      providerEdgeID: null,
      role: null,
      requiredTool: null,
    };
  }
  return {
    fieldName: details.fieldName,
    inputName: details.inputName,
    placeholder: details.placeholder,
    providerEdgeID: details.providerEdgeId ?? null,
    role: details.role ?? null,
    requiredTool: details.requiredTool ?? null,
  };
}
export function workflowValidation(value: pb.ValidateResponse | undefined): WorkflowValidation {
  if (value === undefined) throw new ContractError("Workflow validation result is required.");
  return { valid: value.valid, errors: value.errors.map(workflowValidationError) };
}

function workflowApplicability(value: pb.SelectorApplicability | undefined): WorkflowSelectorApplicability {
  if (value === undefined) throw new ContractError("Workflow selector applicability is required.");
  return {
    available: value.available,
    parameterVisible: value.parameterVisible,
    reason: workflowSelectorReason.decode(value.reason),
  };
}

export function workflowWiring(value: pb.DerivedWiring | undefined): WorkflowDerivedWiring {
  if (value === undefined) throw new ContractError("Workflow derived wiring is required.");
  return {
    nodes: value.nodes.map((node) => ({
      nodeID: node.nodeId,
      possibleProvisionFields: node.possibleProvisionFields.map(workflowOutputField),
      joinOutputFields: node.joinOutputFields.map(workflowOutputField),
    })),
    transitionGroups: value.transitionGroups.map((group) => ({
      transitionGroupID: group.transitionGroupId,
      requiredProvisionFields: group.requiredProvisionFields.map(workflowOutputField),
    })),
    edges: value.edges.map((edge) => ({
      edgeID: edge.edgeId,
      inputBindings: edge.inputBindings.map((binding) => ({
        name: binding.name,
        source: binding.source,
        field: binding.field,
      })),
      requiredProvisionFields: edge.requiredProvisionFields.map(workflowOutputField),
      requiredProviderFields: edge.requiredProviderFields.map(workflowOutputField),
      assigneeSelectionApplicability: workflowApplicability(edge.assigneeSelectionApplicability),
      thinkingSelectionApplicability: workflowApplicability(edge.thinkingSelectionApplicability),
    })),
    diagnostics: value.diagnostics.map(workflowValidationError),
  };
}

function workflowOutputField(value: pb.OutputField) {
  return { name: value.name, description: value.description };
}

export function workflowValidationResults(
  values: readonly pb.ModeValidationResult[],
): WorkflowGraphValidationResults {
  const results: Partial<Record<"draft" | "task_creation" | "execution", WorkflowValidation>> = {};
  for (const value of values)
    results[workflowValidationMode.decode(value.mode)] = workflowValidation(value.result);
  return results;
}

function graphEntity(value: pb.GraphEntityReference): WorkflowGraphEntityReference {
  return { entityType: workflowGraphEntityType.decode(value.entityType), entityID: value.entityId };
}

function graphImpact(value: pb.GraphSaveImpact | undefined): WorkflowGraphSaveImpact {
  if (value === undefined) throw new ContractError("Workflow graph save impact is required.");
  return {
    removedNodeGroupCount: Number(value.removedNodeGroupCount),
    removedNodeCount: Number(value.removedNodeCount),
    removedTransitionGroupCount: Number(value.removedTransitionGroupCount),
    removedEdgeCount: Number(value.removedEdgeCount),
    removedEntities: value.removedEntities.map(graphEntity),
    nodeTaskReferenceCount: Number(value.nodeTaskReferenceCount),
    edgeTaskReferenceCount: Number(value.edgeTaskReferenceCount),
    activeCurrentNodeCount: Number(value.activeCurrentNodeCount),
    pendingApprovalCount: Number(value.pendingApprovalCount),
    startNodeChangeCount: Number(value.startNodeChangeCount),
    lastTerminalChangeCount: Number(value.lastTerminalChangeCount),
    taskReferencedNodeKindChangeCount: Number(value.taskReferencedNodeKindChangeCount),
  };
}

function graphBlocker(value: pb.GraphSaveBlocker): WorkflowGraphSaveBlocker {
  return {
    code: value.code,
    message: value.message,
    count: Number(value.count),
    affectedEntities: value.affectedEntities.map(graphEntity),
  };
}

export function workflowSavePreview(
  value: pb.GraphSavePreviewSuccess | pb.GraphSaveSuccess,
): WorkflowGraphSavePreview {
  return {
    changed: value.changed,
    currentVersion: Number(value.currentVersion),
    validationResults: workflowValidationResults(value.validationResults),
    impact: graphImpact(value.impact),
    blockers: value.blockers.map(graphBlocker),
    canSave: value.canSave,
    confirmationRequired: value.confirmationRequired,
  };
}

export function workflowDeleteImpact(value: pb.DeleteImpact | undefined): WorkflowDeleteImpact {
  if (value === undefined) throw new ContractError("Workflow deletion impact is required.");
  return {
    workflowID: value.workflowId,
    version: Number(value.version),
    projectCount: Number(value.projectCount),
    linkCount: Number(value.linkCount),
    defaultReplacementProjectCount: Number(value.defaultReplacementProjectCount),
    taskCount: Number(value.taskCount),
    currentNodeCount: Number(value.currentNodeCount),
    pendingApprovalCount: Number(value.pendingApprovalCount),
    blockedTaskCount: Number(value.blockedTaskCount),
  };
}
