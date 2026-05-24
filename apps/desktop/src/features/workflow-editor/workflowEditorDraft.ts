/* eslint-disable complexity, max-lines -- The draft reducer and serializers are kept together as one pure state module. */
import type {
  WorkflowContextSource,
  WorkflowDefinition,
  WorkflowEdge,
  WorkflowGraphDraft,
  WorkflowGraphMetadata,
  WorkflowNode,
  WorkflowNodeGroup,
  WorkflowOutputField,
  WorkflowTransitionGroup,
} from "../../api";

export type DraftOutputField = Readonly<{
  rowID: string;
  name: string;
  description: string;
}>;

export type DraftWorkflowNode = Omit<WorkflowNode, "outputFields"> &
  Readonly<{
    outputFields: readonly DraftOutputField[];
  }>;

export type DraftWorkflowDefinition = Omit<WorkflowDefinition, "nodes"> &
  Readonly<{
    nodes: readonly DraftWorkflowNode[];
  }>;

export type WorkflowEditorDraftState = Readonly<{
  acknowledgedConflictDefinitionRevision: number;
  source: WorkflowDefinition;
  draft: DraftWorkflowDefinition;
  conflict: WorkflowDefinition | null;
  version: number;
}>;

export type WorkflowEditorDraftAction =
  | Readonly<{ type: "reset"; source: WorkflowDefinition }>
  | Readonly<{ type: "conflict"; source: WorkflowDefinition }>
  | Readonly<{ type: "keepEditing" }>
  | Readonly<{ type: "reloadConflict" }>
  | Readonly<{ type: "editWorkflowMetadata"; name: string; description: string }>
  | Readonly<{
      type: "editAgentNode";
      nodeID: string;
      patch: Partial<Pick<WorkflowNode, "key" | "name" | "subagentRole" | "promptTemplate">>;
    }>
  | Readonly<{ type: "addOutputField"; nodeID: string }>
  | Readonly<{
      type: "updateOutputField";
      nodeID: string;
      rowID: string;
      patch: Partial<WorkflowOutputField>;
    }>
  | Readonly<{ type: "deleteOutputField"; nodeID: string; rowID: string }>
  | Readonly<{ type: "moveOutputField"; nodeID: string; rowID: string; direction: -1 | 1 }>
  | Readonly<{ type: "reorderOutputField"; nodeID: string; activeRowID: string; overRowID: string }>;

export type WorkflowEditorDirtyState = Readonly<{
  dirty: boolean;
  graphDirty: boolean;
  metadataDirty: boolean;
}>;

export function initializeWorkflowEditorDraft(source: WorkflowDefinition): WorkflowEditorDraftState {
  return {
    acknowledgedConflictDefinitionRevision: 0,
    conflict: null,
    draft: draftDefinitionFromSource(source),
    source,
    version: 0,
  };
}

export function workflowEditorDraftReducer(
  state: WorkflowEditorDraftState,
  action: WorkflowEditorDraftAction,
): WorkflowEditorDraftState {
  switch (action.type) {
    case "reset":
      return initializeWorkflowEditorDraft(action.source);
    case "conflict":
      return { ...state, conflict: action.source };
    case "keepEditing":
      return {
        ...state,
        acknowledgedConflictDefinitionRevision: state.conflict?.workflow.definitionRevision ?? 0,
        conflict: null,
      };
    case "reloadConflict":
      return state.conflict === null ? state : initializeWorkflowEditorDraft(state.conflict);
    case "editWorkflowMetadata":
      return nextDraftState(state, {
        ...state.draft,
        workflow: { ...state.draft.workflow, name: action.name, description: action.description },
      });
    case "editAgentNode":
      return editDraftNode(state, action.nodeID, (node) => {
        if (node.kind !== "agent") {
          return node;
        }
        return { ...node, ...action.patch };
      });
    case "addOutputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        outputFields: [
          ...node.outputFields,
          {
            description: "",
            name: "",
            rowID: [node.id, "field", state.version.toString(), node.outputFields.length.toString()].join(
              ":",
            ),
          },
        ],
      }));
    case "updateOutputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        outputFields: node.outputFields.map((field) =>
          field.rowID === action.rowID ? { ...field, ...action.patch } : field,
        ),
      }));
    case "deleteOutputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        outputFields: node.outputFields.filter((field) => field.rowID !== action.rowID),
      }));
    case "moveOutputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        outputFields: moveRow(node.outputFields, action.rowID, action.direction),
      }));
    case "reorderOutputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        outputFields: reorderRow(node.outputFields, action.activeRowID, action.overRowID),
      }));
  }
}

export function draftDefinitionFromSource(source: WorkflowDefinition): DraftWorkflowDefinition {
  return {
    ...source,
    nodes: source.nodes.map((node) => ({
      ...node,
      outputFields: node.outputFields.map((field, index) => ({
        ...field,
        rowID: [node.id, "field", index.toString()].join(":"),
      })),
    })),
  };
}

export function workflowDefinitionFromDraft(draft: DraftWorkflowDefinition): WorkflowDefinition {
  return {
    ...draft,
    nodes: draft.nodes.map((node) => ({
      ...node,
      outputFields: node.outputFields.map(({ name, description }) => ({ name, description })),
    })),
  };
}

export function workflowEditorDirtyState(state: WorkflowEditorDraftState): WorkflowEditorDirtyState {
  const metadataDirty =
    state.draft.workflow.name !== state.source.workflow.name ||
    state.draft.workflow.description !== state.source.workflow.description;
  const graphDirty = !workflowGraphsEqual(workflowDefinitionFromDraft(state.draft), state.source);
  return { dirty: metadataDirty || graphDirty, graphDirty, metadataDirty };
}

export function workflowEditorDraftGraph(state: WorkflowEditorDraftState): WorkflowGraphDraft {
  const definition = workflowDefinitionFromDraft(state.draft);
  return {
    edges: definition.edges.map((edge) => ({
      contextMode: edge.contextMode,
      contextSource: edge.contextSource,
      id: edge.id,
      inputBindings: edge.inputBindings,
      key: edge.key,
      outputRequirements: edge.outputRequirements,
      requiresApproval: edge.requiresApproval,
      targetNodeID: edge.targetNodeID,
      transitionGroupID: edge.transitionGroupID,
    })),
    nodeGroups: definition.nodeGroups.map((group) => ({ id: group.id, key: group.key, name: group.name })),
    nodes: definition.nodes.map((node) => ({
      groupID: node.groupID,
      groupKey: node.groupKey,
      id: node.id,
      key: node.key,
      kind: node.kind,
      name: node.name,
      outputFields: node.outputFields,
      promptTemplate: node.promptTemplate,
      subagentRole: node.subagentRole,
    })),
    transitionGroups: definition.transitionGroups.map((group) => ({
      id: group.id,
      name: group.name,
      sourceNodeID: group.sourceNodeID,
      transitionID: group.transitionID,
    })),
  };
}

export function workflowEditorDraftMetadata(state: WorkflowEditorDraftState): WorkflowGraphMetadata {
  return { description: state.draft.workflow.description, name: state.draft.workflow.name };
}

function nextDraftState(
  state: WorkflowEditorDraftState,
  draft: DraftWorkflowDefinition,
): WorkflowEditorDraftState {
  return { ...state, draft, version: state.version + 1 };
}

function editDraftNode(
  state: WorkflowEditorDraftState,
  nodeID: string,
  edit: (node: DraftWorkflowNode, nodes: readonly DraftWorkflowNode[]) => DraftWorkflowNode,
): WorkflowEditorDraftState {
  let nextEdges = state.draft.edges;
  const nodes = state.draft.nodes.map((node) => {
    if (node.id !== nodeID) {
      return node;
    }
    const edited = edit(node, state.draft.nodes);
    if (edited.key !== node.key) {
      nextEdges = selectedNodeCascadeEdges({
        edges: nextEdges,
        newKey: edited.key,
        nodeID: node.id,
        nodes: state.draft.nodes,
        oldKey: node.key,
      });
    }
    return edited;
  });
  return nextDraftState(state, { ...state.draft, edges: nextEdges, nodes });
}

type SelectedNodeCascadeRequest = Readonly<{
  edges: readonly WorkflowEdge[];
  nodeID: string;
  oldKey: string;
  newKey: string;
  nodes: readonly DraftWorkflowNode[];
}>;

function selectedNodeCascadeEdges(req: SelectedNodeCascadeRequest): readonly WorkflowEdge[] {
  const { edges, nodeID, oldKey, newKey, nodes } = req;
  const oldKeyOwners = nodes.filter((item) => item.key === oldKey);
  if (oldKeyOwners.length !== 1 || oldKeyOwners[0]?.id !== nodeID) {
    return edges;
  }
  return edges.map((edge) =>
    edge.contextSource.kind === "selected_node" && edge.contextSource.nodeKey === oldKey
      ? { ...edge, contextSource: { ...edge.contextSource, nodeKey: newKey } }
      : edge,
  );
}

function moveRow<T extends Readonly<{ rowID: string }>>(
  rows: readonly T[],
  rowID: string,
  direction: -1 | 1,
): readonly T[] {
  const index = rows.findIndex((row) => row.rowID === rowID);
  const nextIndex = index + direction;
  if (index < 0 || nextIndex < 0 || nextIndex >= rows.length) {
    return rows;
  }
  const next = [...rows];
  const [item] = next.splice(index, 1);
  if (item === undefined) {
    return rows;
  }
  next.splice(nextIndex, 0, item);
  return next;
}

function reorderRow<T extends Readonly<{ rowID: string }>>(
  rows: readonly T[],
  activeRowID: string,
  overRowID: string,
): readonly T[] {
  const activeIndex = rows.findIndex((row) => row.rowID === activeRowID);
  const overIndex = rows.findIndex((row) => row.rowID === overRowID);
  if (activeIndex < 0 || overIndex < 0 || activeIndex === overIndex) {
    return rows;
  }
  const next = [...rows];
  const [item] = next.splice(activeIndex, 1);
  if (item === undefined) {
    return rows;
  }
  next.splice(overIndex, 0, item);
  return next;
}

function workflowGraphsEqual(left: WorkflowDefinition, right: WorkflowDefinition): boolean {
  return (
    nodeGroupsEqual(left.nodeGroups, right.nodeGroups) &&
    nodesEqual(left.nodes, right.nodes) &&
    transitionGroupsEqual(left.transitionGroups, right.transitionGroups) &&
    edgesEqual(left.edges, right.edges)
  );
}

function nodeGroupsEqual(left: readonly WorkflowNodeGroup[], right: readonly WorkflowNodeGroup[]): boolean {
  return sameLengthAndEvery(left, right, (a, b) => a.id === b.id && a.key === b.key && a.name === b.name);
}

function nodesEqual(left: readonly WorkflowNode[], right: readonly WorkflowNode[]): boolean {
  return sameLengthAndEvery(
    left,
    right,
    (a, b) =>
      a.id === b.id &&
      a.key === b.key &&
      a.kind === b.kind &&
      a.name === b.name &&
      a.groupID === b.groupID &&
      a.groupKey === b.groupKey &&
      a.subagentRole === b.subagentRole &&
      a.promptTemplate === b.promptTemplate &&
      outputFieldsEqual(a.outputFields, b.outputFields),
  );
}

function transitionGroupsEqual(
  left: readonly WorkflowTransitionGroup[],
  right: readonly WorkflowTransitionGroup[],
): boolean {
  return sameLengthAndEvery(
    left,
    right,
    (a, b) =>
      a.id === b.id &&
      a.sourceNodeID === b.sourceNodeID &&
      a.transitionID === b.transitionID &&
      a.name === b.name,
  );
}

function edgesEqual(left: readonly WorkflowEdge[], right: readonly WorkflowEdge[]): boolean {
  return sameLengthAndEvery(
    left,
    right,
    (a, b) =>
      a.id === b.id &&
      a.transitionGroupID === b.transitionGroupID &&
      a.key === b.key &&
      a.targetNodeID === b.targetNodeID &&
      a.requiresApproval === b.requiresApproval &&
      a.contextMode === b.contextMode &&
      contextSourceEqual(a.contextSource, b.contextSource) &&
      inputBindingsEqual(a.inputBindings, b.inputBindings) &&
      outputRequirementsEqual(a.outputRequirements, b.outputRequirements),
  );
}

function outputFieldsEqual(
  left: readonly WorkflowOutputField[],
  right: readonly WorkflowOutputField[],
): boolean {
  return sameLengthAndEvery(left, right, (a, b) => a.name === b.name && a.description === b.description);
}

function inputBindingsEqual(
  left: readonly WorkflowDefinition["edges"][number]["inputBindings"][number][],
  right: readonly WorkflowDefinition["edges"][number]["inputBindings"][number][],
): boolean {
  return sameLengthAndEvery(
    left,
    right,
    (a, b) => a.name === b.name && a.source === b.source && a.field === b.field,
  );
}

function outputRequirementsEqual(
  left: readonly WorkflowDefinition["edges"][number]["outputRequirements"][number][],
  right: readonly WorkflowDefinition["edges"][number]["outputRequirements"][number][],
): boolean {
  return sameLengthAndEvery(left, right, (a, b) => a.fieldName === b.fieldName);
}

function contextSourceEqual(left: WorkflowContextSource, right: WorkflowContextSource): boolean {
  return left.kind === right.kind && left.nodeKey === right.nodeKey;
}

function sameLengthAndEvery<T>(
  left: readonly T[],
  right: readonly T[],
  equal: (left: T, right: T) => boolean,
): boolean {
  return (
    left.length === right.length &&
    left.every((item, index) => right[index] !== undefined && equal(item, right[index]))
  );
}
