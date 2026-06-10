/* eslint-disable complexity -- The draft reducer and serializers are kept together as one pure state module. */
import type {
  WorkflowContextSource,
  WorkflowDefinition,
  WorkflowEdge,
  WorkflowGraphDraft,
  WorkflowGraphMetadata,
  WorkflowInputField,
  WorkflowNode,
  WorkflowNodeGroup,
  WorkflowParameter,
  WorkflowTransitionGroup,
} from "../../api";
import {
  addWorkflowNode,
  addWorkflowNodeToGroup,
  connectWorkflowNodes,
  createWorkflowNodeGroupFromNode,
  deleteWorkflowEdge,
  deleteWorkflowNode,
  deleteWorkflowNodeGroup,
  editWorkflowEdgeRoute,
  extractWorkflowNodeFromGroup,
  reconnectWorkflowEdge,
  removeWorkflowNodeFromGroup,
  type AddWorkflowNodeInput,
  type AddWorkflowNodeToGroupInput,
  type ConnectWorkflowNodesInput,
  type CreateWorkflowNodeGroupInput,
  type EditWorkflowEdgeRouteInput,
  type ExtractWorkflowNodeFromGroupInput,
  type ReconnectWorkflowEdgeInput,
  type WorkflowEditorCascadeSummary,
  type WorkflowEditorGraphMutationResult,
  type WorkflowEditorSelection,
} from "./workflowEditorGraphMutations";

export type DraftInputField = Readonly<{
  rowID: string;
  name: string;
  description: string;
}>;

export type DraftWorkflowParameter = WorkflowParameter &
  Readonly<{
    rowID?: string;
  }>;

export type DraftWorkflowNode = Omit<WorkflowNode, "inputFields"> &
  Readonly<{
    inputFields: readonly DraftInputField[];
  }>;

export type DraftWorkflowEdge = Omit<WorkflowEdge, "parameters"> &
  Readonly<{
    parameters: readonly DraftWorkflowParameter[];
  }>;

export type DraftWorkflowDefinition = Omit<WorkflowDefinition, "edges" | "nodes"> &
  Readonly<{
    edges: readonly DraftWorkflowEdge[];
    nodes: readonly DraftWorkflowNode[];
  }>;

export type WorkflowEditorDraftState = Readonly<{
  acknowledgedConflictVersion: number;
  source: WorkflowDefinition;
  draft: DraftWorkflowDefinition;
  conflict: WorkflowDefinition | null;
  graphVersion: number;
  lastTopologyMutation: WorkflowEditorTopologyMutation | null;
  version: number;
}>;

export type WorkflowEditorTopologyMutation = Readonly<{
  summary: WorkflowEditorCascadeSummary;
  warnings: readonly string[];
  nextSelection: WorkflowEditorSelection;
}>;

export type WorkflowEditorDraftAction =
  | Readonly<{ type: "reset"; source: WorkflowDefinition }>
  | Readonly<{ type: "conflict"; source: WorkflowDefinition }>
  | Readonly<{ type: "keepEditing" }>
  | Readonly<{ type: "reloadConflict" }>
  | Readonly<{ type: "editWorkflowMetadata"; name: string; description: string }>
  | Readonly<{
      type: "editNodeIdentity";
      nodeID: string;
      patch: Partial<Pick<WorkflowNode, "key" | "name">>;
    }>
  | Readonly<{
      type: "editAgentNode";
      nodeID: string;
      patch: Partial<Pick<WorkflowNode, "key" | "name" | "subagentRole" | "promptTemplate">>;
    }>
  | Readonly<{ type: "addInputField"; nodeID: string }>
  | Readonly<{
      type: "updateInputField";
      nodeID: string;
      rowID: string;
      patch: Partial<WorkflowInputField>;
    }>
  | Readonly<{ type: "deleteInputField"; nodeID: string; rowID: string }>
  | Readonly<{ type: "reorderInputField"; nodeID: string; activeRowID: string; overRowID: string }>
  | Readonly<{ type: "assignJoinInputProvider"; nodeID: string; inputName: string; providerEdgeID: string }>
  | Readonly<{ type: "editEdgePrompt"; edgeID: string; promptTemplate: string }>
  | Readonly<{ type: "addEdgeParameter"; edgeID: string }>
  | Readonly<{
      type: "updateEdgeParameter";
      edgeID: string;
      parameterRowID: string;
      patch: Partial<WorkflowParameter>;
    }>
  | Readonly<{ type: "deleteEdgeParameter"; edgeID: string; parameterRowID: string }>
  | Readonly<{ type: "reorderEdgeParameter"; edgeID: string; activeRowID: string; overRowID: string }>
  | Readonly<{ type: "addNode"; input: AddWorkflowNodeInput }>
  | Readonly<{ type: "deleteNode"; nodeID: string }>
  | Readonly<{ type: "connectNodes"; input: ConnectWorkflowNodesInput }>
  | Readonly<{ type: "reconnectEdge"; input: ReconnectWorkflowEdgeInput }>
  | Readonly<{ type: "deleteEdge"; edgeID: string }>
  | Readonly<{ type: "editEdgeRoute"; input: EditWorkflowEdgeRouteInput }>
  | Readonly<{ type: "createNodeGroupFromNode"; input: CreateWorkflowNodeGroupInput }>
  | Readonly<{ type: "addNodeToGroup"; input: AddWorkflowNodeToGroupInput }>
  | Readonly<{ type: "deleteNodeGroup"; groupID: string }>
  | Readonly<{ type: "extractNodeFromGroup"; input: ExtractWorkflowNodeFromGroupInput }>
  | Readonly<{ type: "removeNodeFromGroup"; nodeID: string }>;

export type WorkflowEditorDirtyState = Readonly<{
  dirty: boolean;
  graphDirty: boolean;
  metadataDirty: boolean;
}>;

export function initializeWorkflowEditorDraft(source: WorkflowDefinition): WorkflowEditorDraftState {
  return {
    acknowledgedConflictVersion: 0,
    conflict: null,
    draft: draftDefinitionFromSource(source),
    graphVersion: 0,
    lastTopologyMutation: null,
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
        acknowledgedConflictVersion: state.conflict?.workflow.version ?? 0,
        conflict: null,
      };
    case "reloadConflict":
      return state.conflict === null ? state : initializeWorkflowEditorDraft(state.conflict);
    case "editWorkflowMetadata":
      return nextDraftState(
        state,
        {
          ...state.draft,
          workflow: { ...state.draft.workflow, name: action.name, description: action.description },
        },
        false,
      );
    case "editNodeIdentity":
      return editDraftNode(state, action.nodeID, (node) => {
        if (node.kind !== "start" && node.kind !== "terminal" && node.kind !== "agent") {
          return node;
        }
        return { ...node, ...action.patch };
      });
    case "editAgentNode":
      return editDraftNode(state, action.nodeID, (node) => {
        if (node.kind !== "agent") {
          return node;
        }
        return { ...node, ...action.patch };
      });
    case "addInputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        inputFields: [
          {
            description: "",
            name: "",
            rowID: [node.id, "input", state.version.toString(), node.inputFields.length.toString()].join(
              ":",
            ),
          },
          ...node.inputFields,
        ],
      }));
    case "updateInputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        inputFields: node.inputFields.map((field) =>
          field.rowID === action.rowID ? { ...field, ...action.patch } : field,
        ),
      }));
    case "deleteInputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        inputFields: node.inputFields.filter((field) => field.rowID !== action.rowID),
      }));
    case "reorderInputField":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        inputFields: reorderRow(node.inputFields, action.activeRowID, action.overRowID),
      }));
    case "assignJoinInputProvider":
      return editDraftNode(state, action.nodeID, (node) => ({
        ...node,
        joinInputProviders: assignJoinInputProvider(node.joinInputProviders, action.inputName, action.providerEdgeID),
      }));
    case "editEdgePrompt":
      return editDraftEdge(state, action.edgeID, (edge) => ({
        ...edge,
        promptTemplate: action.promptTemplate,
      }));
    case "addEdgeParameter":
      return editDraftEdge(state, action.edgeID, (edge) => ({
        ...edge,
        parameters: [
          {
            description: "",
            key: "",
            rowID: [edge.id, "parameter", state.version.toString(), edge.parameters.length.toString()].join(
              ":",
            ),
          },
          ...edge.parameters,
        ],
      }));
    case "updateEdgeParameter":
      return editDraftEdge(state, action.edgeID, (edge) => ({
        ...edge,
        parameters: edge.parameters.map((parameter) =>
          parameter.rowID === action.parameterRowID ? { ...parameter, ...action.patch } : parameter,
        ),
      }));
    case "deleteEdgeParameter":
      return editDraftEdge(state, action.edgeID, (edge) => ({
        ...edge,
        parameters: edge.parameters.filter((parameter) => parameter.rowID !== action.parameterRowID),
      }));
    case "reorderEdgeParameter":
      return editDraftEdge(state, action.edgeID, (edge) => ({
        ...edge,
        parameters: reorderParameterRows(edge.parameters, action.activeRowID, action.overRowID),
      }));
    case "addNode":
      return applyTopologyMutation(state, addWorkflowNode(state.draft, action.input));
    case "deleteNode":
      return applyTopologyMutation(state, deleteWorkflowNode(state.draft, action.nodeID));
    case "connectNodes":
      return applyTopologyMutation(state, connectWorkflowNodes(state.draft, action.input));
    case "reconnectEdge":
      return applyTopologyMutation(state, reconnectWorkflowEdge(state.draft, action.input));
    case "deleteEdge":
      return applyTopologyMutation(state, deleteWorkflowEdge(state.draft, action.edgeID));
    case "editEdgeRoute":
      return applyTopologyMutation(state, editWorkflowEdgeRoute(state.draft, action.input));
    case "createNodeGroupFromNode":
      return applyTopologyMutation(state, createWorkflowNodeGroupFromNode(state.draft, action.input));
    case "addNodeToGroup":
      return applyTopologyMutation(state, addWorkflowNodeToGroup(state.draft, action.input));
    case "deleteNodeGroup":
      return applyTopologyMutation(state, deleteWorkflowNodeGroup(state.draft, action.groupID));
    case "extractNodeFromGroup":
      return applyTopologyMutation(state, extractWorkflowNodeFromGroup(state.draft, action.input));
    case "removeNodeFromGroup":
      return applyTopologyMutation(state, removeWorkflowNodeFromGroup(state.draft, action.nodeID));
  }
}

export function draftDefinitionFromSource(source: WorkflowDefinition): DraftWorkflowDefinition {
  return {
    ...source,
    edges: source.edges.map(draftEdgeWithParameterRowIDs),
    nodes: source.nodes.map((node) => ({
      ...node,
      inputFields: node.inputFields.map((field, index) => ({
        ...field,
        rowID: [node.id, "input", index.toString()].join(":"),
      })),
    })),
  };
}

export function workflowDefinitionFromDraft(draft: DraftWorkflowDefinition): WorkflowDefinition {
  return {
    ...draft,
    edges: draft.edges.map((edge) => ({
      ...edge,
      parameters: edge.parameters.map(({ description, key }) => ({ description, key })),
    })),
    nodes: draft.nodes.map((node) => ({
      ...node,
      inputFields: node.inputFields.map(({ name, description }) => ({ name, description })),
      outputFields: node.outputFields,
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
      key: edge.key,
      parameters: edge.parameters.map(({ description, key }) => ({ description, key })),
      promptTemplate: edge.promptTemplate,
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
      inputFields: node.inputFields,
      joinInputProviders: node.joinInputProviders,
      promptTemplate: node.promptTemplate,
      subagentRole: node.subagentRole,
    })),
    transitionGroups: definition.transitionGroups.map((group) => ({
      description: group.description,
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
  graphChanged = true,
  lastTopologyMutation: WorkflowEditorTopologyMutation | null = null,
): WorkflowEditorDraftState {
  return {
    ...state,
    draft,
    graphVersion: graphChanged ? state.graphVersion + 1 : state.graphVersion,
    lastTopologyMutation,
    version: state.version + 1,
  };
}

function applyTopologyMutation(
  state: WorkflowEditorDraftState,
  mutation: WorkflowEditorGraphMutationResult,
): WorkflowEditorDraftState {
  const lastTopologyMutation = {
    nextSelection: mutation.nextSelection,
    summary: mutation.summary,
    warnings: mutation.warnings,
  };
  if (mutation.draft === state.draft) {
    return { ...state, lastTopologyMutation };
  }
  return nextDraftState(state, draftDefinitionFromSource(mutation.draft), true, {
    ...lastTopologyMutation,
  });
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

function editDraftEdge(
  state: WorkflowEditorDraftState,
  edgeID: string,
  edit: (edge: DraftWorkflowEdge, edges: readonly DraftWorkflowEdge[]) => DraftWorkflowEdge,
): WorkflowEditorDraftState {
  const edgeIndex = state.draft.edges.findIndex((edge) => edge.id === edgeID);
  if (edgeIndex < 0) {
    return state;
  }
  const edges = state.draft.edges.map((edge, index) =>
    index === edgeIndex ? draftEdgeWithParameterRowIDs(edit(edge, state.draft.edges)) : edge,
  );
  return nextDraftState(state, { ...state.draft, edges });
}

function draftEdgeWithParameterRowIDs(edge: WorkflowEdge): DraftWorkflowEdge {
  return {
    ...edge,
    parameters: edge.parameters.map((parameter, index) => ({
      ...parameter,
      rowID: draftParameterRowID(parameter) ?? [edge.id, "parameter", index.toString()].join(":"),
    })),
  };
}

function draftParameterRowID(parameter: WorkflowParameter): string | undefined {
  return "rowID" in parameter && typeof parameter.rowID === "string" ? parameter.rowID : undefined;
}

type SelectedNodeCascadeRequest = Readonly<{
  edges: readonly DraftWorkflowEdge[];
  nodeID: string;
  oldKey: string;
  newKey: string;
  nodes: readonly DraftWorkflowNode[];
}>;

function selectedNodeCascadeEdges(req: SelectedNodeCascadeRequest): readonly DraftWorkflowEdge[] {
  const { edges, nodeID, oldKey, newKey, nodes } = req;
  const oldKeyOwners = nodes.filter((item) => item.key === oldKey);
  const oldKeyOwner = oldKeyOwners.at(0);
  if (oldKeyOwners.length !== 1 || oldKeyOwner?.id !== nodeID) {
    return edges;
  }
  return edges.map((edge) =>
    edge.contextSource.kind === "selected_node" && edge.contextSource.nodeKey === oldKey
      ? { ...edge, contextSource: { ...edge.contextSource, nodeKey: newKey } }
      : edge,
  );
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

function reorderParameterRows(
  rows: readonly DraftWorkflowParameter[],
  activeRowID: string,
  overRowID: string,
): readonly DraftWorkflowParameter[] {
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
      inputFieldsEqual(a.inputFields, b.inputFields) &&
      joinInputProvidersEqual(a.joinInputProviders, b.joinInputProviders),
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
      a.name === b.name &&
      a.description === b.description,
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
      a.promptTemplate === b.promptTemplate &&
      parametersEqual(a.parameters, b.parameters),
  );
}

function inputFieldsEqual(
  left: readonly WorkflowDefinition["nodes"][number]["inputFields"][number][],
  right: readonly WorkflowDefinition["nodes"][number]["inputFields"][number][],
): boolean {
  return sameLengthAndEvery(left, right, (a, b) => a.name === b.name && a.description === b.description);
}

function parametersEqual(
  left: readonly WorkflowDefinition["edges"][number]["parameters"][number][],
  right: readonly WorkflowDefinition["edges"][number]["parameters"][number][],
): boolean {
  return sameLengthAndEvery(left, right, (a, b) => a.key === b.key && a.description === b.description);
}

function assignJoinInputProvider(
  providers: WorkflowDefinition["nodes"][number]["joinInputProviders"],
  inputName: string,
  providerEdgeID: string,
): WorkflowDefinition["nodes"][number]["joinInputProviders"] {
  const updated = { inputName, providerEdgeID };
  const providerIndex = providers.findIndex((provider) => provider.inputName === inputName);
  if (providerIndex === -1) {
    return [...providers, updated];
  }
  return providers.map((provider, index) => (index === providerIndex ? updated : provider));
}

function joinInputProvidersEqual(
  left: readonly WorkflowDefinition["nodes"][number]["joinInputProviders"][number][],
  right: readonly WorkflowDefinition["nodes"][number]["joinInputProviders"][number][],
): boolean {
  if (left.length !== right.length) {
    return false;
  }
  const rightByInputName = new Map(right.map((provider) => [provider.inputName, provider.providerEdgeID]));
  return left.every((provider) => rightByInputName.get(provider.inputName) === provider.providerEdgeID);
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
