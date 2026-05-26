/* eslint-disable max-lines -- Sidebar keeps read-only and draft-backed workflow inspector paths together for now. */
import { useId, useState, useSyncExternalStore } from "react";
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
import { GripVertical, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import type {
  ServerReadiness,
  WorkflowDefinition,
  WorkflowEdge,
  WorkflowNode,
  WorkflowNodeGroup,
  WorkflowValidation,
} from "../../api";
import { queryKeys } from "../../app/queryKeys";
import type { WorkflowInspectorSelection } from "../../app/sidebarContext";
import { Button, MarkdownText, SelectField, TextArea, TextInput, type SelectFieldOption } from "../../ui";
import { cx } from "../../ui/classes";
import { fieldInputClassName } from "../../ui/Field";
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
  const definition = {
    ...workflowDefinitionFromDraft(controller.draft),
    derivedWiring: controller.derivedWiring,
  };
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
    if (node.kind === "join") {
      return (
        <JoinNodeDraftDetails
          controller={controller}
          definition={definition}
          node={node}
          validation={validation}
        />
      );
    }
    return <NodeDetails definition={definition} node={{ ...node, outputFields: [] }} validation={validation} />;
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
  const assigneeOptions = useWorkflowAssigneeOptions(definition);
  const errors = validation.errors.filter(
    (error) => error.nodeID === node.id || error.relatedIDs.includes(node.id),
  );
  return (
    <InspectorStack>
      <DetailSection>
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
        <SelectField
          label={t("workflowEditor.assignee")}
          onValueChange={(value) => {
            controller.dispatch({
              nodeID: node.id,
              patch: { subagentRole: value },
              type: "editAgentNode",
            });
          }}
          options={assigneeOptions}
          placeholder={t("workflowEditor.selectAssignee")}
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
      <EditableInputFields controller={controller} node={node} />
      <FieldSummary
        fields={derivedNodeWiring(definition, node.id).possibleProvisionFields}
        title={t("workflowEditor.provides")}
      />
      <ValidationDetails errors={errors} />
    </InspectorStack>
  );
}

function JoinNodeDraftDetails({
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
  const errors = validation.errors.filter(
    (error) => error.nodeID === node.id || error.relatedIDs.includes(node.id),
  );
  return (
    <InspectorStack>
      <DetailSection title={t("workflowEditor.inspectorIdentity")}>
        <DetailRow label={t("workflowEditor.key")} mono value={node.key} />
      </DetailSection>
      <EditableJoinProviders controller={controller} definition={definition} node={node} />
      <ValidationDetails errors={errors} />
    </InspectorStack>
  );
}

function EditableInputFields({
  controller,
  node,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  node: DraftWorkflowNode;
}>) {
  const { t } = useTranslation();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  return (
    <DetailSection title={t("workflowEditor.requiredInputs")}>
      <Button
        onClick={() => {
          controller.dispatch({ nodeID: node.id, type: "addInputField" });
        }}
        variant="secondary"
      >
        {t("workflowEditor.addRequiredInput")}
      </Button>
      {node.inputFields.length === 0 ? (
        <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.none")}</p>
      ) : null}
      <DndContext
        collisionDetection={closestCenter}
        onDragEnd={(event) => {
          reorderInputField(controller, node.id, event);
        }}
        sensors={sensors}
      >
        <SortableContext
          items={node.inputFields.map((field) => field.rowID)}
          strategy={verticalListSortingStrategy}
        >
          <div className="grid gap-[var(--space-3)]">
            {node.inputFields.map((field) => (
              <SortableInputField
                controller={controller}
                field={field}
                key={field.rowID}
                nodeID={node.id}
              />
            ))}
          </div>
        </SortableContext>
      </DndContext>
    </DetailSection>
  );
}

function SortableInputField({
  controller,
  field,
  nodeID,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  field: DraftWorkflowNode["inputFields"][number];
  nodeID: string;
}>) {
  const { t } = useTranslation();
  const [isEditingName, setIsEditingName] = useState(field.name.length === 0);
  const descriptionID = useId();
  const { attributes, listeners, setActivatorNodeRef, setNodeRef, transform, transition } = useSortable({
    id: field.rowID,
  });
  const style = {
    transform:
      transform === null
        ? undefined
        : `translate3d(${transform.x.toString()}px, ${transform.y.toString()}px, 0)`,
    transition,
  };
  return (
    <div
      className="workflow-editor-input-field relative grid gap-[var(--space-2)] rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)]"
      data-input-field-name={field.name}
      data-testid="workflow-input-field"
      ref={setNodeRef}
      style={style}
    >
      <div
        aria-label={t("workflowEditor.reorderInputField")}
        className="absolute inset-0 cursor-grab rounded-[inherit] outline-none focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_srgb,var(--color-primary)_35%,transparent)] active:cursor-grabbing"
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
      />
      <div className="pointer-events-none relative grid gap-[var(--space-2)]">
        <div className="flex min-w-0 items-center gap-[var(--space-2)]">
          <GripVertical
            aria-hidden="true"
            className="shrink-0 text-[var(--color-muted)]"
            size={18}
            strokeWidth={1.8}
          />
          {isEditingName ? (
            <input
              aria-label={t("workflowEditor.inputFieldName")}
              autoFocus
              className="app-region-no-drag pointer-events-auto min-w-0 flex-1 rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] px-[var(--space-2)] py-[var(--space-1)] font-bold text-[var(--color-on-island)] outline-none focus:border-[var(--color-primary)]"
              onBlur={() => {
                setIsEditingName(false);
              }}
              onChange={(event) => {
                controller.dispatch({
                  nodeID,
                  patch: { name: event.target.value.replaceAll("\n", " ") },
                  rowID: field.rowID,
                  type: "updateInputField",
                });
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === "Escape") {
                  event.preventDefault();
                  setIsEditingName(false);
                }
              }}
              type="text"
              value={field.name}
            />
          ) : (
            <button
              className="pointer-events-auto min-w-0 flex-1 truncate rounded-[var(--radius-m)] border border-transparent bg-transparent px-0 py-[var(--space-1)] text-left font-bold text-[var(--color-on-island)] outline-none focus:border-[var(--color-outline)] focus:bg-[var(--color-island-1)] focus:px-[var(--space-2)]"
              onClick={() => {
                setIsEditingName(true);
              }}
              type="button"
            >
              {field.name.length > 0 ? field.name : t("workflowEditor.inputFieldName")}
            </button>
          )}
          <Button
            aria-label={t("workflowEditor.deleteField")}
            className="pointer-events-auto grid h-8 w-8 shrink-0 place-items-center rounded-full !border-transparent !bg-transparent !p-0"
            onClick={() => {
              controller.dispatch({ nodeID, rowID: field.rowID, type: "deleteInputField" });
            }}
            variant="danger"
          >
            <Trash2 aria-hidden="true" size={17} strokeWidth={1.9} />
          </Button>
        </div>
        <div className="pointer-events-auto">
          <label className="sr-only" htmlFor={descriptionID}>
            {t("workflowEditor.inputFieldDescription")}
          </label>
          <input
            className={cx(fieldInputClassName, "px-[var(--space-2)] py-[var(--space-2)]")}
            id={descriptionID}
            onChange={(event) => {
              controller.dispatch({
                nodeID,
                patch: { description: event.target.value },
                rowID: field.rowID,
                type: "updateInputField",
              });
            }}
            placeholder={t("workflowEditor.inputFieldDescription")}
            value={field.description}
          />
        </div>
      </div>
    </div>
  );
}

function reorderInputField(
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
    type: "reorderInputField",
  });
}

function EditableJoinProviders({
  controller,
  definition,
  node,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  definition: WorkflowDefinition;
  node: DraftWorkflowNode;
}>) {
  const { t } = useTranslation();
  const requiredFields = derivedNodeWiring(definition, node.id).joinOutputFields;
  const providerByInput = new Map(node.joinInputProviders.map((provider) => [provider.inputName, provider]));
  return (
    <DetailSection title={t("workflowEditor.joinProviders")}>
      {requiredFields.length === 0 ? (
        <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.none")}</p>
      ) : (
        <div className="grid gap-[var(--space-3)]">
          {requiredFields.map((field) => {
            const selectedEdgeID = providerByInput.get(field.name)?.providerEdgeID ?? "";
            return (
              <SelectField
                hint={field.description}
                key={field.name}
                label={field.name}
                onValueChange={(value) => {
                  controller.dispatch({
                    inputName: field.name,
                    nodeID: node.id,
                    providerEdgeID: value,
                    type: "assignJoinInputProvider",
                  });
                }}
                options={joinProviderOptions(definition, node.id, selectedEdgeID)}
                placeholder={t("workflowEditor.selectProvider")}
                value={selectedEdgeID}
              />
            );
          })}
        </div>
      )}
    </DetailSection>
  );
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
  const errors = validation.errors.filter(
    (error) => error.nodeID === node.id || error.relatedIDs.includes(node.id),
  );
  const derivedNode = derivedNodeWiring(definition, node.id);
  return (
    <InspectorStack>
      <DetailSection>
        <DetailRow label={t("workflowEditor.key")} mono value={node.key} />
        {node.kind === "agent" ? (
          <>
            <DetailRow
              label={t("workflowEditor.assignee")}
              value={fallbackLabel(t("workflowEditor.none"), node.subagentRole)}
            />
            <PromptPreview prompt={node.promptTemplate} />
          </>
        ) : null}
      </DetailSection>
      {node.kind === "agent" ? (
        <>
          <FieldSummary fields={node.inputFields} title={t("workflowEditor.requiredInputs")} />
          <FieldSummary fields={derivedNode.possibleProvisionFields} title={t("workflowEditor.provides")} />
        </>
      ) : null}
      {node.kind === "join" ? (
        <>
          <FieldSummary fields={derivedNode.joinOutputFields} title={t("workflowEditor.joinRequiredInputs")} />
          <JoinProviders definition={definition} node={node} />
        </>
      ) : null}
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
  const derivedEdge = derivedEdgeWiring(definition, edge.id);
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
      <Bindings bindings={derivedEdge.inputBindings} />
      <FieldSummary
        fields={derivedEdge.requiredProvisionFields}
        title={t("workflowEditor.derivedProvisionRequirements")}
      />
      {derivedEdge.requiredProviderFields.length === 0 ? null : (
        <FieldSummary
          fields={derivedEdge.requiredProviderFields}
          title={t("workflowEditor.providerRequirements")}
        />
      )}
      <ValidationDetails errors={details.directErrors} title={t("workflowEditor.edgeErrors")} />
      <ValidationDetails errors={details.groupErrors} title={t("workflowEditor.transitionGroupErrors")} />
    </InspectorStack>
  );
}

function FieldSummary({
  fields,
  title,
}: Readonly<{ fields: readonly { name: string; description: string }[]; title: string }>) {
  const { t } = useTranslation();
  return (
    <DetailSection title={title}>
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

function JoinProviders({
  definition,
  node,
}: Readonly<{ definition: WorkflowDefinition; node: WorkflowNode }>) {
  const { t } = useTranslation();
  const fields = derivedNodeWiring(definition, node.id).joinOutputFields;
  const providerByInput = new Map(node.joinInputProviders.map((provider) => [provider.inputName, provider]));
  if (fields.length === 0) {
    return null;
  }
  return (
    <DetailSection title={t("workflowEditor.joinProviders")}>
      <ul className="m-0 grid gap-[var(--space-2)] p-0">
        {fields.map((field) => {
          const providerEdgeID = providerByInput.get(field.name)?.providerEdgeID ?? "";
          const provider = providerEdgeLabel(definition, providerEdgeID);
          return (
            <li className="list-none text-sm" key={field.name}>
              <span className="font-mono">{field.name}</span>
              <span className="text-[var(--color-muted)]"> = {provider || t("workflowEditor.none")}</span>
            </li>
          );
        })}
      </ul>
    </DetailSection>
  );
}

function Bindings({ bindings }: Readonly<{ bindings: WorkflowEdge["inputBindings"] }>) {
  const { t } = useTranslation();
  return (
    <DetailSection title={t("workflowEditor.derivedInputBindings")}>
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

function derivedNodeWiring(definition: WorkflowDefinition, nodeID: string) {
  return (
    definition.derivedWiring.nodes.find((wiring) => wiring.nodeID === nodeID) ?? {
      joinOutputFields: [],
      nodeID,
      possibleProvisionFields: [],
    }
  );
}

function derivedEdgeWiring(definition: WorkflowDefinition, edgeID: string) {
  return (
    definition.derivedWiring.edges.find((wiring) => wiring.edgeID === edgeID) ?? {
      edgeID,
      inputBindings: [],
      requiredProviderFields: [],
      requiredProvisionFields: [],
    }
  );
}

function joinProviderOptions(
  definition: WorkflowDefinition,
  joinNodeID: string,
  selectedEdgeID: string,
): readonly SelectFieldOption[] {
  const options = definition.edges
    .filter((edge) => edge.targetNodeID === joinNodeID)
    .map((edge) => ({
      label: providerEdgeLabel(definition, edge.id),
      textValue: providerEdgeLabel(definition, edge.id),
      value: edge.id,
    }));
  if (selectedEdgeID.length === 0 || options.some((option) => option.value === selectedEdgeID)) {
    return options;
  }
  return [
    ...options,
    {
      disabled: true,
      label: selectedEdgeID,
      textValue: selectedEdgeID,
      value: selectedEdgeID,
    },
  ];
}

function providerEdgeLabel(definition: WorkflowDefinition, edgeID: string): string {
  const edge = definition.edges.find((item) => item.id === edgeID);
  if (edge === undefined) {
    return edgeID;
  }
  const group = transitionGroupByID(definition, edge.transitionGroupID);
  const source = group === undefined ? undefined : nodeByID(definition, group.sourceNodeID);
  const sourceLabel = fallbackLabel(edge.key, source?.name, source?.key);
  return `${sourceLabel} / ${edge.key}`;
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

function useCachedServerReadiness(): ServerReadiness | undefined {
  const queryClient = useQueryClient();
  return useSyncExternalStore(
    (onStoreChange) => queryClient.getQueryCache().subscribe(onStoreChange),
    () => queryClient.getQueryData<ServerReadiness>(queryKeys.readiness),
    () => queryClient.getQueryData<ServerReadiness>(queryKeys.readiness),
  );
}

function useWorkflowAssigneeOptions(definition: WorkflowDefinition): readonly SelectFieldOption[] {
  const readiness = useCachedServerReadiness();
  return workflowAssigneeOptions(definition, readiness?.subagentRoles ?? []);
}

function workflowAssigneeOptions(
  definition: WorkflowDefinition,
  roles: ServerReadiness["subagentRoles"],
): readonly SelectFieldOption[] {
  const roleNames = new Set<string>();
  roleNames.add("default");
  for (const role of roles) {
    if (role.name.trim().length > 0) {
      roleNames.add(role.name);
    }
  }
  const workflowRoleNames = definition.nodes
    .filter((node) => node.kind === "agent")
    .map((node) => node.subagentRole.trim())
    .filter((role) => role.length > 0)
    .sort((left, right) => left.localeCompare(right));
  for (const role of workflowRoleNames) {
    roleNames.add(role);
  }
  return [...roleNames].map((role) => ({ label: role, textValue: role, value: role }));
}
