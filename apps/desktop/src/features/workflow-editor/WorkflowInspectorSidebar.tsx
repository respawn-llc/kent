/* eslint-disable max-lines -- Sidebar keeps read-only and draft-backed workflow inspector paths together for now. */
import { useSyncExternalStore } from "react";
import {
  closestCenter,
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type {
  WorkflowDefinition,
  WorkflowEdge,
  WorkflowNode,
  WorkflowNodeGroup,
  WorkflowValidation,
} from "../../api";
import { queryKeys } from "../../app/queryKeys";
import type { WorkflowInspectorSelection } from "../../app/sidebarContext";
import { Button, MarkdownText, TextArea, TextInput } from "../../ui";
import {
  DetailRow,
  DetailSection,
  InspectorStack,
  MissingEntity,
  ValidationDetails,
} from "./WorkflowInspectorPrimitives";
import { fallbackLabel, nodeByID, transitionGroupByID } from "./workflowInspectorModel";
import { workflowDefinitionFromDraft, type DraftWorkflowNode } from "./workflowEditorDraft";
import {
  useWorkflowEditorDraftController,
  type WorkflowEditorDraftController,
} from "./workflowEditorDraftBridgeCore";

export function WorkflowInspectorSidebar({
  selection,
  workflowID,
}: Readonly<{
  selection: WorkflowInspectorSelection;
  workflowID: string;
}>) {
  const { t } = useTranslation();
  const controller = useWorkflowEditorDraftController(workflowID);
  const definition = useCachedWorkflowDefinition(workflowID);
  const validation = useCachedWorkflowValidation(workflowID);
  if (controller !== null) {
    return <WorkflowDraftInspectorContent controller={controller} selection={selection} />;
  }
  if (definition === undefined) {
    return <p className="text-[var(--color-muted)]">{t("workflowEditor.inspectorUnavailable")}</p>;
  }
  return (
    <WorkflowInspectorContent
      definition={definition}
      selection={selection}
      validation={validation ?? { valid: true, errors: [] }}
    />
  );
}

function WorkflowDraftInspectorContent({
  controller,
  selection,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  selection: WorkflowInspectorSelection;
}>) {
  const definition = workflowDefinitionFromDraft(controller.draft);
  const validation = controller.draftValidation ??
    controller.executionValidation ?? { errors: [], valid: true };
  if (selection.kind === "workflow") {
    return <WorkflowDraftDetails controller={controller} />;
  }
  if (selection.kind === "node") {
    const node = controller.draft.nodes.find((item) => item.id === selection.nodeID);
    if (node === undefined) {
      return <MissingEntity entityID={selection.nodeID} />;
    }
    if (node.kind === "agent") {
      return (
        <AgentNodeDraftDetails
          controller={controller}
          definition={definition}
          node={node}
          validation={validation}
        />
      );
    }
    return <NodeDetails definition={definition} node={node} validation={validation} />;
  }
  if (selection.kind === "group") {
    const group = definition.nodeGroups.find((item) => item.id === selection.groupID);
    return group === undefined ? (
      <MissingEntity entityID={selection.groupID} />
    ) : (
      <GroupDetails definition={definition} group={group} validation={validation} />
    );
  }
  const edge = definition.edges.find((item) => item.id === selection.edgeID);
  return edge === undefined ? (
    <MissingEntity entityID={selection.edgeID} />
  ) : (
    <EdgeDetails definition={definition} edge={edge} validation={validation} />
  );
}

function WorkflowDraftDetails({ controller }: Readonly<{ controller: WorkflowEditorDraftController }>) {
  const { t } = useTranslation();
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.workflowSettings")}>
        <TextInput
          label={t("workflowLibrary.name")}
          onChange={(event) => {
            controller.dispatch({
              description: controller.draft.workflow.description,
              name: event.target.value,
              type: "editWorkflowMetadata",
            });
          }}
          value={controller.draft.workflow.name}
        />
        <TextArea
          label={t("workflowLibrary.description")}
          onChange={(event) => {
            controller.dispatch({
              description: event.target.value,
              name: controller.draft.workflow.name,
              type: "editWorkflowMetadata",
            });
          }}
          value={controller.draft.workflow.description}
        />
      </DetailSection>
      <DetailSection title={t("workflowEditor.inspectorOverview")}>
        <DetailRow
          label={t("workflowEditor.version")}
          value={controller.draft.workflow.version.toString()}
        />
        <DetailRow
          label={t("workflowEditor.version")}
          value={controller.draft.workflow.version.toString()}
        />
        <DetailRow label={t("workflowEditor.nodeCount")} value={controller.draft.nodes.length.toString()} />
        <DetailRow label={t("workflowEditor.edgeCount")} value={controller.draft.edges.length.toString()} />
        <DetailRow
          label={t("workflowEditor.groupCount")}
          value={controller.draft.nodeGroups.length.toString()}
        />
      </DetailSection>
      <ValidationDetails errors={controller.draftValidation?.errors ?? []} />
    </InspectorStack>
  );
}

function AgentNodeDraftDetails({
  controller,
  definition,
  node,
  validation,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  definition: WorkflowDefinition;
  node: DraftWorkflowNode;
  validation: WorkflowValidation;
}>) {
  const { t } = useTranslation();
  const group = definition.nodeGroups.find((item) => item.id === node.groupID);
  const errors = validation.errors.filter(
    (error) => error.nodeID === node.id || error.relatedIDs.includes(node.id),
  );
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.inspectorIdentity")}>
        <TextInput
          label={t("workflowEditor.displayName")}
          onChange={(event) => {
            controller.dispatch({
              nodeID: node.id,
              patch: { name: event.target.value },
              type: "editAgentNode",
            });
          }}
          value={node.name}
        />
        <TextInput
          label={t("workflowEditor.key")}
          onChange={(event) => {
            controller.dispatch({
              nodeID: node.id,
              patch: { key: event.target.value },
              type: "editAgentNode",
            });
          }}
          value={node.key}
        />
        <DetailRow label={t("workflowEditor.kind")} value={node.kind} />
        <DetailRow label={t("workflowEditor.id")} mono value={node.id} />
        <DetailRow
          label={t("workflowEditor.group")}
          value={fallbackLabel(t("workflowEditor.none"), group?.name, group?.key)}
        />
      </DetailSection>
      <DetailSection title={t("workflowEditor.behavior")}>
        <TextInput
          label={t("workflowEditor.assignee")}
          onChange={(event) => {
            controller.dispatch({
              nodeID: node.id,
              patch: { subagentRole: event.target.value },
              type: "editAgentNode",
            });
          }}
          value={node.subagentRole}
        />
        <TextArea
          label={t("workflowEditor.prompt")}
          onChange={(event) => {
            controller.dispatch({
              nodeID: node.id,
              patch: { promptTemplate: event.target.value },
              type: "editAgentNode",
            });
          }}
          value={node.promptTemplate}
        />
      </DetailSection>
      <EditableOutputFields controller={controller} node={node} />
      <ValidationDetails errors={errors} />
    </InspectorStack>
  );
}

function EditableOutputFields({
  controller,
  node,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  node: DraftWorkflowNode;
}>) {
  const { t } = useTranslation();
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  return (
    <DetailSection title={t("workflowEditor.outputFields")}>
      <div className="grid gap-[var(--space-3)]">
        {node.outputFields.length === 0 ? (
          <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.none")}</p>
        ) : null}
        <DndContext
          collisionDetection={closestCenter}
          onDragEnd={(event) => {
            reorderOutputField(controller, node.id, event);
          }}
          sensors={sensors}
        >
          <SortableContext
            items={node.outputFields.map((field) => field.rowID)}
            strategy={verticalListSortingStrategy}
          >
            <div className="grid gap-[var(--space-3)]">
              {node.outputFields.map((field, index) => (
                <SortableOutputField
                  controller={controller}
                  field={field}
                  first={index === 0}
                  key={field.rowID}
                  last={index === node.outputFields.length - 1}
                  nodeID={node.id}
                />
              ))}
            </div>
          </SortableContext>
        </DndContext>
        <Button
          onClick={() => {
            controller.dispatch({ nodeID: node.id, type: "addOutputField" });
          }}
          variant="secondary"
        >
          {t("workflowEditor.addOutputField")}
        </Button>
      </div>
    </DetailSection>
  );
}

function SortableOutputField({
  controller,
  field,
  first,
  last,
  nodeID,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  field: DraftWorkflowNode["outputFields"][number];
  first: boolean;
  last: boolean;
  nodeID: string;
}>) {
  const { t } = useTranslation();
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id: field.rowID });
  const style = {
    transform:
      transform === null
        ? undefined
        : `translate3d(${transform.x.toString()}px, ${transform.y.toString()}px, 0)`,
    transition,
  };
  return (
    <div
      className="grid gap-[var(--space-2)] rounded-[var(--radius-m)] border border-[var(--color-outline)] p-[var(--space-3)]"
      ref={setNodeRef}
      style={style}
    >
      <button
        className="cursor-grab rounded-[var(--radius-s)] border border-[var(--color-outline)] bg-transparent px-2 py-1 text-left text-xs text-[var(--color-muted)] active:cursor-grabbing"
        type="button"
        {...attributes}
        {...listeners}
      >
        {t("workflowEditor.dragField")}
      </button>
      <TextInput
        label={t("workflowEditor.outputFieldName")}
        onChange={(event) => {
          controller.dispatch({
            nodeID,
            patch: { name: event.target.value },
            rowID: field.rowID,
            type: "updateOutputField",
          });
        }}
        value={field.name}
      />
      <TextInput
        label={t("workflowEditor.outputFieldDescription")}
        onChange={(event) => {
          controller.dispatch({
            nodeID,
            patch: { description: event.target.value },
            rowID: field.rowID,
            type: "updateOutputField",
          });
        }}
        value={field.description}
      />
      <div className="flex flex-wrap gap-[var(--space-2)]">
        <Button
          disabled={first}
          onClick={() => {
            controller.dispatch({ direction: -1, nodeID, rowID: field.rowID, type: "moveOutputField" });
          }}
          variant="ghost"
        >
          {t("workflowEditor.moveFieldUp")}
        </Button>
        <Button
          disabled={last}
          onClick={() => {
            controller.dispatch({ direction: 1, nodeID, rowID: field.rowID, type: "moveOutputField" });
          }}
          variant="ghost"
        >
          {t("workflowEditor.moveFieldDown")}
        </Button>
        <Button
          onClick={() => {
            controller.dispatch({ nodeID, rowID: field.rowID, type: "deleteOutputField" });
          }}
          variant="danger"
        >
          {t("workflowEditor.deleteField")}
        </Button>
      </div>
    </div>
  );
}

function reorderOutputField(
  controller: WorkflowEditorDraftController,
  nodeID: string,
  event: DragEndEvent,
): void {
  const overID = event.over?.id;
  if (overID === undefined || event.active.id === overID) {
    return;
  }
  controller.dispatch({
    activeRowID: event.active.id.toString(),
    nodeID,
    overRowID: overID.toString(),
    type: "reorderOutputField",
  });
}

function WorkflowInspectorContent({
  definition,
  selection,
  validation,
}: Readonly<{
  definition: WorkflowDefinition;
  selection: WorkflowInspectorSelection;
  validation: WorkflowValidation;
}>) {
  if (selection.kind === "workflow") {
    return <WorkflowDetails definition={definition} validation={validation} />;
  }
  if (selection.kind === "node") {
    const node = definition.nodes.find((item) => item.id === selection.nodeID);
    return node === undefined ? (
      <MissingEntity entityID={selection.nodeID} />
    ) : (
      <NodeDetails definition={definition} node={node} validation={validation} />
    );
  }
  if (selection.kind === "group") {
    const group = definition.nodeGroups.find((item) => item.id === selection.groupID);
    return group === undefined ? (
      <MissingEntity entityID={selection.groupID} />
    ) : (
      <GroupDetails definition={definition} group={group} validation={validation} />
    );
  }
  const edge = definition.edges.find((item) => item.id === selection.edgeID);
  return edge === undefined ? (
    <MissingEntity entityID={selection.edgeID} />
  ) : (
    <EdgeDetails definition={definition} edge={edge} validation={validation} />
  );
}

function WorkflowDetails({
  definition,
  validation,
}: Readonly<{ definition: WorkflowDefinition; validation: WorkflowValidation }>) {
  const { t } = useTranslation();
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.inspectorOverview")}>
        <DetailRow
          label={t("workflowEditor.version")}
          value={definition.workflow.version.toString()}
        />
        <DetailRow label={t("workflowEditor.nodeCount")} value={definition.nodes.length.toString()} />
        <DetailRow label={t("workflowEditor.edgeCount")} value={definition.edges.length.toString()} />
        <DetailRow label={t("workflowEditor.groupCount")} value={definition.nodeGroups.length.toString()} />
      </DetailSection>
      {definition.workflow.description.length > 0 ? (
        <DetailSection title={t("workflowEditor.description")}>
          <p className="m-0 text-sm text-[var(--color-on-island)]">{definition.workflow.description}</p>
        </DetailSection>
      ) : null}
      <ValidationDetails errors={validation.errors} />
    </InspectorStack>
  );
}

function NodeDetails({
  definition,
  node,
  validation,
}: Readonly<{ definition: WorkflowDefinition; node: WorkflowNode; validation: WorkflowValidation }>) {
  const { t } = useTranslation();
  const group = definition.nodeGroups.find((item) => item.id === node.groupID);
  const errors = validation.errors.filter(
    (error) => error.nodeID === node.id || error.relatedIDs.includes(node.id),
  );
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.inspectorIdentity")}>
        <DetailRow label={t("workflowEditor.kind")} value={node.kind} />
        <DetailRow label={t("workflowEditor.key")} mono value={node.key} />
        <DetailRow label={t("workflowEditor.id")} mono value={node.id} />
        <DetailRow
          label={t("workflowEditor.group")}
          value={fallbackLabel(t("workflowEditor.none"), group?.name, group?.key)}
        />
      </DetailSection>
      {node.kind === "agent" ? (
        <DetailSection title={t("workflowEditor.behavior")}>
          <DetailRow
            label={t("workflowEditor.assignee")}
            value={fallbackLabel(t("workflowEditor.none"), node.subagentRole)}
          />
          <PromptPreview prompt={node.promptTemplate} />
        </DetailSection>
      ) : null}
      <OutputFields fields={node.outputFields} />
      <ValidationDetails errors={errors} />
    </InspectorStack>
  );
}

function GroupDetails({
  definition,
  group,
  validation,
}: Readonly<{ definition: WorkflowDefinition; group: WorkflowNodeGroup; validation: WorkflowValidation }>) {
  const { t } = useTranslation();
  const members = definition.nodes.filter((node) => node.groupID === group.id);
  const errors = validation.errors.filter((error) => error.relatedIDs.includes(group.id));
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.inspectorIdentity")}>
        <DetailRow label={t("workflowEditor.key")} mono value={group.key} />
        <DetailRow label={t("workflowEditor.id")} mono value={group.id} />
        <DetailRow label={t("workflowEditor.sortOrder")} value={group.sortOrder.toString()} />
      </DetailSection>
      <DetailSection title={t("workflowEditor.members")}>
        {members.length === 0 ? (
          <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.emptyGroup")}</p>
        ) : (
          <ul className="m-0 grid gap-[var(--space-1)] p-0">
            {members.map((node) => (
              <li className="list-none text-sm" key={node.id}>
                {fallbackLabel(node.key, node.name)}{" "}
                <span className="font-mono text-[var(--color-muted)]">({node.kind})</span>
              </li>
            ))}
          </ul>
        )}
      </DetailSection>
      <ValidationDetails errors={errors} />
    </InspectorStack>
  );
}

function EdgeDetails({
  definition,
  edge,
  validation,
}: Readonly<{ definition: WorkflowDefinition; edge: WorkflowEdge; validation: WorkflowValidation }>) {
  const { t } = useTranslation();
  const details = edgeDetails(definition, edge, validation);
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.inspectorIdentity")}>
        <DetailRow label={t("workflowEditor.key")} mono value={edge.key} />
        <DetailRow label={t("workflowEditor.id")} mono value={edge.id} />
        <DetailRow label={t("workflowEditor.transitionID")} mono value={details.transitionID} />
        <DetailRow label={t("workflowEditor.transitionGroup")} value={details.transitionGroupLabel} />
      </DetailSection>
      <DetailSection title={t("workflowEditor.route")}>
        <DetailRow label={t("workflowEditor.sourceNode")} value={details.sourceLabel} />
        <DetailRow label={t("workflowEditor.targetNode")} value={details.targetLabel} />
        <DetailRow
          label={t("workflowEditor.contextMode")}
          value={formatContextModeLabel(edge.contextMode, t)}
        />
        <DetailRow label={t("workflowEditor.contextSource")} value={formatContextSourceLabel(edge, t)} />
        <DetailRow
          label={t("workflowEditor.requiresApproval")}
          value={edge.requiresApproval ? t("workflowEditor.required") : t("workflowEditor.none")}
        />
      </DetailSection>
      <Bindings bindings={edge.inputBindings} />
      <Requirements requirements={edge.outputRequirements} />
      <ValidationDetails errors={details.directErrors} title={t("workflowEditor.edgeErrors")} />
      <ValidationDetails errors={details.groupErrors} title={t("workflowEditor.transitionGroupErrors")} />
    </InspectorStack>
  );
}

function OutputFields({ fields }: Readonly<{ fields: WorkflowNode["outputFields"] }>) {
  const { t } = useTranslation();
  return (
    <DetailSection title={t("workflowEditor.outputFields")}>
      {fields.length === 0 ? (
        <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.none")}</p>
      ) : (
        <ul className="m-0 grid gap-[var(--space-2)] p-0">
          {fields.map((field, index) => (
            <li className="list-none" key={`${field.name}:${index.toString()}`}>
              <span className="font-mono text-sm">{field.name}</span>
              {field.description.length > 0 ? (
                <p className="m-0 text-sm text-[var(--color-muted)]">{field.description}</p>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </DetailSection>
  );
}

function Bindings({ bindings }: Readonly<{ bindings: WorkflowEdge["inputBindings"] }>) {
  const { t } = useTranslation();
  return (
    <DetailSection title={t("workflowEditor.inputBindings")}>
      {bindings.length === 0 ? (
        <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.none")}</p>
      ) : (
        <ul className="m-0 grid gap-[var(--space-1)] p-0">
          {bindings.map((binding) => (
            <li className="list-none text-sm" key={`${binding.name}:${binding.source}:${binding.field}`}>
              <span className="font-mono">{binding.name}</span> = {binding.source}.{binding.field}
            </li>
          ))}
        </ul>
      )}
    </DetailSection>
  );
}

function Requirements({ requirements }: Readonly<{ requirements: WorkflowEdge["outputRequirements"] }>) {
  const { t } = useTranslation();
  return (
    <DetailSection title={t("workflowEditor.outputRequirements")}>
      {requirements.length === 0 ? (
        <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.none")}</p>
      ) : (
        <ul className="m-0 grid gap-[var(--space-1)] p-0">
          {requirements.map((requirement) => (
            <li className="list-none font-mono text-sm" key={requirement.fieldName}>
              {requirement.fieldName}
            </li>
          ))}
        </ul>
      )}
    </DetailSection>
  );
}

function PromptPreview({ prompt }: Readonly<{ prompt: string }>) {
  const { t } = useTranslation();
  if (prompt.length === 0) {
    return <DetailRow label={t("workflowEditor.prompt")} value={t("workflowEditor.none")} />;
  }
  return (
    <div className="grid gap-[var(--space-1)]">
      <span className="text-xs font-bold uppercase tracking-[0.14em] text-[var(--color-muted)]">
        {t("workflowEditor.prompt")}
      </span>
      <div className="rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-2)] text-sm">
        <MarkdownText value={prompt} />
      </div>
    </div>
  );
}

function edgeDetails(definition: WorkflowDefinition, edge: WorkflowEdge, validation: WorkflowValidation) {
  const group = transitionGroupByID(definition, edge.transitionGroupID);
  const source = group === undefined ? undefined : nodeByID(definition, group.sourceNodeID);
  const target = nodeByID(definition, edge.targetNodeID);
  return {
    directErrors: validation.errors.filter((error) => error.edgeID === edge.id),
    groupErrors: validation.errors.filter(
      (error) => error.edgeID !== edge.id && error.transitionGroupID === edge.transitionGroupID,
    ),
    sourceLabel: fallbackLabel("", source?.name, source?.key),
    targetLabel: fallbackLabel("", target?.name, target?.key),
    transitionGroupLabel: fallbackLabel("", group?.name, group?.id),
    transitionID: group?.transitionID ?? "",
  };
}

function formatContextModeLabel(mode: string, translate: Translate): string {
  if (mode === "new_session") {
    return translate("workflowEditor.contextModeNewSession");
  }
  if (mode === "continue_session") {
    return translate("workflowEditor.contextModeContinueSession");
  }
  if (mode === "compact_and_continue_session") {
    return translate("workflowEditor.contextModeCompactContinueSession");
  }
  return mode;
}

function formatContextSourceLabel(edge: WorkflowEdge, translate: Translate): string {
  if (edge.contextSource.kind === "selected_node") {
    return edge.contextSource.nodeKey.length > 0
      ? translate("workflowEditor.contextSourceNode", { nodeKey: edge.contextSource.nodeKey })
      : translate("workflowEditor.contextSourceSelected");
  }
  return translate("workflowEditor.contextSourceImmediate");
}

type Translate = ReturnType<typeof useTranslation>["t"];

function useCachedWorkflowDefinition(workflowID: string): WorkflowDefinition | undefined {
  const queryKey = queryKeys.workflowDefinition(workflowID);
  const queryClient = useQueryClient();
  return useSyncExternalStore(
    (onStoreChange) => queryClient.getQueryCache().subscribe(onStoreChange),
    () => queryClient.getQueryData<WorkflowDefinition>(queryKey),
    () => queryClient.getQueryData<WorkflowDefinition>(queryKey),
  );
}

function useCachedWorkflowValidation(workflowID: string): WorkflowValidation | undefined {
  const queryKey = queryKeys.workflowValidation(workflowID, "execution");
  const queryClient = useQueryClient();
  return useSyncExternalStore(
    (onStoreChange) => queryClient.getQueryCache().subscribe(onStoreChange),
    () => queryClient.getQueryData<WorkflowValidation>(queryKey),
    () => queryClient.getQueryData<WorkflowValidation>(queryKey),
  );
}
