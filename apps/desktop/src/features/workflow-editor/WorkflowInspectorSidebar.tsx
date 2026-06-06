/* eslint-disable max-lines -- Sidebar keeps read-only and draft-backed workflow inspector paths together for now. */
import { useCallback, useEffect, useId, useRef, useSyncExternalStore } from "react";
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
  WorkflowContextSource,
  WorkflowDefinition,
  WorkflowEdge,
  WorkflowNode,
  WorkflowNodeGroup,
  WorkflowParameter,
  WorkflowTransitionGroup,
  WorkflowValidation,
} from "../../api";
import { queryKeys } from "../../app/queryKeys";
import type { WorkflowInspectorSelection } from "../../app/sidebarContext";
import {
  Button,
  Checkbox,
  DisabledInteractionGuard,
  identifierInputAttributes,
  IslandSurface,
  MarkdownText,
  SelectField,
  TextArea,
  TextInput,
  TooltipProvider,
  type SelectFieldOption,
} from "../../ui";
import { cx } from "../../ui/classes";
import { fieldInputClassName } from "../../ui/Field";
import {
  DetailRow,
  DetailSection,
  InspectorStack,
  MissingEntity,
  ValidationDetails,
} from "./WorkflowInspectorPrimitives";
import { WorkflowEdgeRouteGraphic } from "./WorkflowEdgeRouteGraphic";
import {
  fallbackLabel,
  nodeByID,
  transitionGroupByID,
  transitionGroupIsFanOut,
} from "./workflowInspectorModel";
import { workflowDefinitionFromDraft, type DraftWorkflowNode } from "./workflowEditorDraft";
import {
  useWorkflowEditorDraftController,
  type WorkflowEditorDraftController,
} from "./workflowEditorDraftBridgeCore";
import {
  workflowPromptTemplatePlaceholders,
  type PromptTemplatePlaceholder,
} from "./workflowPromptTemplatePlaceholders";

export function WorkflowInspectorSidebar({
  onMissingSelectedNode,
  selection,
  workflowID,
}: Readonly<{
  onMissingSelectedNode?: (() => void) | undefined;
  selection: WorkflowInspectorSelection;
  workflowID: string;
}>) {
  const { t } = useTranslation();
  const controller = useWorkflowEditorDraftController(workflowID);
  const definition = useCachedWorkflowDefinition(workflowID);
  const validation = useCachedWorkflowValidation(workflowID);
  const selectedNodeMissing = selectedNodeNoLongerExists({
    controller,
    definition,
    selection,
  });
  useEffect(() => {
    if (selectedNodeMissing) {
      onMissingSelectedNode?.();
    }
  }, [onMissingSelectedNode, selectedNodeMissing]);
  if (selectedNodeMissing && onMissingSelectedNode !== undefined) {
    return null;
  }
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

function selectedNodeNoLongerExists({
  controller,
  definition,
  selection,
}: Readonly<{
  controller: WorkflowEditorDraftController | null;
  definition: WorkflowDefinition | undefined;
  selection: WorkflowInspectorSelection;
}>): boolean {
  if (selection.kind !== "node") {
    return false;
  }
  const nodes = controller === null ? definition?.nodes : workflowDefinitionFromDraft(controller.draft).nodes;
  return nodes !== undefined && !nodes.some((node) => node.id === selection.nodeID);
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
  const validation = controller.dirty.graphDirty
    ? controller.draftValidation ?? emptyWorkflowValidation
    : controller.draftValidation ?? controller.executionValidation ?? emptyWorkflowValidation;
  if (selection.kind === "workflow") {
    return <WorkflowDraftDetails controller={controller} />;
  }
  if (selection.kind === "node") {
    return (
      <WorkflowDraftNodeDetails
        controller={controller}
        definition={definition}
        nodeID={selection.nodeID}
        validation={validation}
      />
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
    <EdgeDraftDetails controller={controller} definition={definition} edge={edge} validation={validation} />
  );
}

function WorkflowDraftNodeDetails({
  controller,
  definition,
  nodeID,
  validation,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  definition: WorkflowDefinition;
  nodeID: string;
  validation: WorkflowValidation;
}>) {
  const node = controller.draft.nodes.find((item) => item.id === nodeID);
  if (node === undefined) {
    return <MissingEntity entityID={nodeID} />;
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
  if (node.kind === "start" || node.kind === "terminal") {
    return <FixedNodeDraftDetails controller={controller} node={node} validation={validation} />;
  }
  return <NodeDetails definition={definition} node={{ ...node, outputFields: [] }} validation={validation} />;
}

function EdgeDraftDetails({
  controller,
  definition,
  edge,
  validation,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  definition: WorkflowDefinition;
  edge: WorkflowEdge;
  validation: WorkflowValidation;
}>) {
  const { t } = useTranslation();
  const details = edgeDetails(definition, edge, validation);
  const derivedEdge = derivedEdgeWiring(definition, edge.id);
  const transitionGroup = transitionGroupByID(definition, edge.transitionGroupID);
  const fanOutTransition = transitionGroupIsFanOut(definition, edge.transitionGroupID);
  const startEdge = details.sourceKind === "start";
  const continuationAvailable = details.sourceKind === "agent" || details.targetKind === "agent";
  const disabledReason = t("workflowEditor.edgeControlNotApplicable");
  const contextModeDisabled = startEdge;
  const contextSourceDisabled = startEdge || edge.contextMode === "new_session" || !continuationAvailable;
  const requiresApprovalDisabled = startEdge;
  return (
    <InspectorStack>
      <DetailSection
        hideTitle
        leading={
          <WorkflowEdgeRouteGraphic
            contextMode={edge.contextMode}
            hasError={details.hasErrors}
            sourceLabel={details.sourceLabel}
            targetLabel={details.targetLabel}
          />
        }
        title={t("workflowEditor.route")}
      >
        <TextInput
          label={t("workflowEditor.transitionText")}
          onChange={(event) => {
            controller.dispatch({
              input: { edgeID: edge.id, transitionName: event.target.value },
              type: "editEdgeRoute",
            });
          }}
          value={transitionGroup?.name ?? ""}
        />
        <TextInput
          {...identifierInputAttributes}
          label={t("workflowEditor.key")}
          onChange={(event) => {
            controller.dispatch({
              input: { edgeID: edge.id, transitionID: event.target.value.replaceAll("\n", " ") },
              type: "editEdgeRoute",
            });
          }}
          value={details.transitionID}
        />
        {fanOutTransition ? (
          <TextInput
            {...identifierInputAttributes}
            label={t("workflowEditor.branchKey")}
            onChange={(event) => {
              controller.dispatch({
                input: { edgeID: edge.id, edgeKey: event.target.value.replaceAll("\n", " ") },
                type: "editEdgeRoute",
              });
            }}
            value={edge.key}
          />
        ) : null}
        <TooltipProvider delayDuration={0}>
          <DisabledInteractionGuard disabled={contextModeDisabled} reason={disabledReason}>
            <SelectField
              disabled={contextModeDisabled}
              label={t("workflowEditor.contextMode")}
              onValueChange={(value) => {
                if (value !== "new_session" && !continuationAvailable) {
                  return;
                }
                controller.dispatch({
                  input: {
                    contextMode: value,
                    contextSource: value === "new_session" ? immediateContextSource : edge.contextSource,
                    edgeID: edge.id,
                  },
                  type: "editEdgeRoute",
                });
              }}
              options={contextModeOptions(t, !continuationAvailable)}
              value={edge.contextMode}
            />
          </DisabledInteractionGuard>
          <DisabledInteractionGuard disabled={contextSourceDisabled} reason={disabledReason}>
            <SelectField
              disabled={contextSourceDisabled}
              label={t("workflowEditor.contextSource")}
              onValueChange={(value) => {
                controller.dispatch({
                  input: { contextSource: contextSourceFromSelectValue(definition, value), edgeID: edge.id },
                  type: "editEdgeRoute",
                });
              }}
              options={contextSourceOptions(definition, edge, t)}
              value={contextSourceSelectValue(definition, edge)}
            />
          </DisabledInteractionGuard>
          <DisabledInteractionGuard disabled={requiresApprovalDisabled} reason={disabledReason}>
            <ApprovalToggle
              checked={edge.requiresApproval}
              disabled={requiresApprovalDisabled}
              label={t("workflowEditor.requiresApproval")}
              onCheckedChange={(checked) => {
                controller.dispatch({ input: { edgeID: edge.id, requiresApproval: checked }, type: "editEdgeRoute" });
              }}
            />
          </DisabledInteractionGuard>
        </TooltipProvider>
      </DetailSection>
      <EdgeInvocationSections
        controller={controller}
        definition={definition}
        edge={edge}
        sourceKind={details.sourceKind}
        targetKind={details.targetKind}
      />
      <DerivedEdgeSections derivedEdge={derivedEdge} />
      <ValidationDetails errors={details.directErrors} title={t("workflowEditor.edgeErrors")} />
      <ValidationDetails errors={details.groupErrors} title={t("workflowEditor.transitionGroupErrors")} />
    </InspectorStack>
  );
}

function EdgeInvocationSections({
  controller,
  definition,
  edge,
  sourceKind,
  targetKind,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  definition: WorkflowDefinition;
  edge: WorkflowEdge;
  sourceKind: string;
  targetKind: string;
}>) {
  const { t } = useTranslation();
  const promptParameters = edgePromptPlaceholderParameters(definition, edge);
  return (
    <>
      {targetKind === "agent" ? (
        <PromptTemplateEditor
          onPromptChange={(promptTemplate) => {
            controller.dispatch({ edgeID: edge.id, promptTemplate, type: "editEdgePrompt" });
          }}
          parameters={promptParameters}
          promptTemplate={edge.promptTemplate}
        />
      ) : null}
      {sourceKind === "agent" ? <EditableEdgeParameters controller={controller} edge={edge} /> : null}
      {sourceKind === "join" && targetKind === "agent" ? (
        <FieldSummary
          fields={parameterSummaryFields(promptParameters)}
          title={t("workflowEditor.joinAggregateParameters")}
        />
      ) : null}
    </>
  );
}

function DerivedEdgeSections({ derivedEdge }: Readonly<{ derivedEdge: ReturnType<typeof derivedEdgeWiring> }>) {
  const { t } = useTranslation();
  return (
    <>
      {derivedEdge.inputBindings.length === 0 ? null : <Bindings bindings={derivedEdge.inputBindings} />}
      {derivedEdge.requiredProvisionFields.length === 0 ? null : (
        <FieldSummary
          fields={derivedEdge.requiredProvisionFields}
          title={t("workflowEditor.derivedProvisionRequirements")}
        />
      )}
      {derivedEdge.requiredProviderFields.length === 0 ? null : (
        <FieldSummary
          fields={derivedEdge.requiredProviderFields}
          title={t("workflowEditor.providerRequirements")}
        />
      )}
    </>
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
          {...identifierInputAttributes}
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
      </DetailSection>
      <FieldSummary
        fields={derivedNodeWiring(definition, node.id).possibleProvisionFields}
        title={t("workflowEditor.provides")}
      />
      <ValidationDetails errors={errors} />
    </InspectorStack>
  );
}

function PromptTemplateEditor({
  onPromptChange,
  parameters,
  promptTemplate,
}: Readonly<{
  onPromptChange: (promptTemplate: string) => void;
  parameters: readonly Pick<WorkflowParameter, "key">[];
  promptTemplate: string;
}>) {
  const { t } = useTranslation();
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const promptInputId = useId();
  const insertPlaceholder = useCallback(
    (placeholder: string) => {
      const textarea = textareaRef.current;
      const currentValue = textarea?.value ?? promptTemplate;
      const hasCursor = textarea !== null && document.activeElement === textarea;
      const insertAt = hasCursor ? textarea.selectionEnd : currentValue.length;
      const nextValue = `${currentValue.slice(0, insertAt)}${placeholder}${currentValue.slice(insertAt)}`;
      const nextCursor = insertAt + placeholder.length;
      onPromptChange(nextValue);
      requestAnimationFrame(() => {
        textarea?.focus({ preventScroll: true });
        textarea?.setSelectionRange(nextCursor, nextCursor);
        if (!hasCursor && textarea !== null) {
          textarea.scrollTop = textarea.scrollHeight;
        }
      });
    },
    [onPromptChange, promptTemplate],
  );
  return (
    <DetailSection title={t("workflowEditor.prompt")}>
      <label className="sr-only" htmlFor={promptInputId}>
        {t("workflowEditor.prompt")}
      </label>
      <div className="grid gap-[var(--space-1)]">
        <textarea
          className={cx(fieldInputClassName, "min-h-24")}
          id={promptInputId}
          onChange={(event) => {
            onPromptChange(event.target.value);
          }}
          ref={textareaRef}
          value={promptTemplate}
        />
        <PromptPlaceholderChips onInsert={insertPlaceholder} parameters={parameters} />
      </div>
    </DetailSection>
  );
}

function PromptPlaceholderChips({
  onInsert,
  parameters,
}: Readonly<{
  onInsert: (placeholder: string) => void;
  parameters: readonly Pick<WorkflowParameter, "key">[];
}>) {
  const { t } = useTranslation();
  const placeholders = workflowPromptTemplatePlaceholders(parameters);
  return (
    <div
      aria-label={t("workflowEditor.promptPlaceholders")}
      className="flex flex-wrap gap-[var(--space-1)]"
      role="group"
    >
      {placeholders.map((placeholder) => (
        <button
          className={cx(promptPlaceholderChipBaseClassName, promptPlaceholderChipToneClassNames[placeholder.tone])}
          data-placeholder-tone={placeholder.tone}
          key={placeholder.value}
          onClick={() => {
            onInsert(placeholder.value);
          }}
          onPointerDown={(event) => {
            event.preventDefault();
          }}
          type="button"
        >
          {placeholder.label}
        </button>
      ))}
    </div>
  );
}

const promptPlaceholderChipBaseClassName =
  "rounded-full border px-[var(--space-1)] py-px text-[11px] font-semibold leading-4 transition-colors focus-visible:outline-none focus-visible:ring-[2px]";

const promptPlaceholderChipToneClassNames = {
  muted:
    "border-[var(--color-outline)] bg-[color-mix(in_srgb,var(--color-on-background)_5%,transparent)] text-[var(--color-muted)] hover:bg-[color-mix(in_srgb,var(--color-on-background)_8%,transparent)] focus-visible:ring-[color-mix(in_srgb,var(--color-muted)_35%,transparent)]",
  primary:
    "border-[color-mix(in_srgb,var(--color-primary)_45%,transparent)] bg-[color-mix(in_srgb,var(--color-primary)_10%,transparent)] text-[var(--color-primary)] hover:bg-[color-mix(in_srgb,var(--color-primary)_16%,transparent)] focus-visible:ring-[color-mix(in_srgb,var(--color-primary)_35%,transparent)]",
} satisfies Record<PromptTemplatePlaceholder["tone"], string>;

function FixedNodeDraftDetails({
  controller,
  node,
  validation,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  node: DraftWorkflowNode;
  validation: WorkflowValidation;
}>) {
  const { t } = useTranslation();
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
              type: "editNodeIdentity",
            });
          }}
          value={node.name}
        />
        <TextInput
          {...identifierInputAttributes}
          label={t("workflowEditor.key")}
          onChange={(event) => {
            controller.dispatch({
              nodeID: node.id,
              patch: { key: event.target.value },
              type: "editNodeIdentity",
            });
          }}
          value={node.key}
        />
      </DetailSection>
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

function EditableEdgeParameters({
  controller,
  edge,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  edge: WorkflowEdge;
}>) {
  const { t } = useTranslation();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  return (
    <DetailSection title={t("workflowEditor.parameters")}>
      <div className="grid gap-[var(--space-3)]">
        <Button
          onClick={() => {
            controller.dispatch({ edgeID: edge.id, type: "addEdgeParameter" });
          }}
          variant="secondary"
        >
          {t("workflowEditor.addParameter")}
        </Button>
        {edge.parameters.length === 0 ? (
          <p className="m-0 text-sm text-[var(--color-muted)]">{t("workflowEditor.none")}</p>
        ) : null}
        <DndContext
          collisionDetection={closestCenter}
          onDragEnd={(event) => {
            reorderEdgeParameter(controller, edge.id, event);
          }}
          sensors={sensors}
        >
          <SortableContext
            items={edge.parameters.map((_parameter, index) => index)}
            strategy={verticalListSortingStrategy}
          >
            <div className="grid gap-[var(--space-3)]">
              {edge.parameters.map((parameter, index) => (
                <SortableEdgeParameter
                  controller={controller}
                  edgeID={edge.id}
                  index={index}
                  key={`${index.toString()}:${parameter.key}`}
                  parameter={parameter}
                />
              ))}
            </div>
          </SortableContext>
        </DndContext>
      </div>
    </DetailSection>
  );
}

function SortableEdgeParameter({
  controller,
  edgeID,
  index,
  parameter,
}: Readonly<{
  controller: WorkflowEditorDraftController;
  edgeID: string;
  index: number;
  parameter: WorkflowParameter;
}>) {
  const { t } = useTranslation();
  const keyID = useId();
  const descriptionID = useId();
  const { attributes, listeners, setActivatorNodeRef, setNodeRef, transform, transition } = useSortable({
    id: index,
  });
  const style = {
    transform:
      transform === null
        ? undefined
        : `translate3d(${transform.x.toString()}px, ${transform.y.toString()}px, 0)`,
    transition,
  };
  return (
    <IslandSurface
      as="div"
      className="workflow-editor-parameter relative grid gap-[var(--space-2)] rounded-[var(--radius-m)] p-[var(--space-3)]"
      data-parameter-key={parameter.key}
      data-testid="workflow-parameter"
      level={1}
      ref={setNodeRef}
      style={style}
    >
      <div
        aria-label={t("workflowEditor.reorderParameter")}
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
          <div className="pointer-events-auto min-w-0 flex-1">
            <label className="sr-only" htmlFor={keyID}>
              {t("workflowEditor.parameterKey")}
            </label>
            <input
              {...identifierInputAttributes}
              autoFocus={parameter.key.length === 0}
              className="app-region-no-drag min-w-0 w-full rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] px-[var(--space-2)] py-[var(--space-1)] font-bold text-[var(--color-on-island)] outline-none focus:border-[var(--color-primary)]"
              id={keyID}
              onChange={(event) => {
                controller.dispatch({
                  edgeID,
                  parameterIndex: index,
                  patch: { key: event.target.value.replaceAll("\n", " ") },
                  type: "updateEdgeParameter",
                });
              }}
              placeholder={t("workflowEditor.parameterKey")}
              type="text"
              value={parameter.key}
            />
          </div>
          <Button
            aria-label={t("workflowEditor.deleteParameter")}
            className="pointer-events-auto grid h-8 w-8 shrink-0 place-items-center rounded-full !border-transparent !bg-transparent !p-0"
            onClick={() => {
              controller.dispatch({ edgeID, parameterIndex: index, type: "deleteEdgeParameter" });
            }}
            variant="danger"
          >
            <Trash2 aria-hidden="true" size={17} strokeWidth={1.9} />
          </Button>
        </div>
        <div className="pointer-events-auto">
          <label className="sr-only" htmlFor={descriptionID}>
            {t("workflowEditor.parameterDescription")}
          </label>
          <input
            className={cx(fieldInputClassName, "px-[var(--space-2)] py-[var(--space-2)]")}
            id={descriptionID}
            onChange={(event) => {
              controller.dispatch({
                edgeID,
                parameterIndex: index,
                patch: { description: event.target.value },
                type: "updateEdgeParameter",
              });
            }}
            placeholder={t("workflowEditor.parameterDescription")}
            value={parameter.description}
          />
        </div>
      </div>
    </IslandSurface>
  );
}

function reorderEdgeParameter(
  controller: WorkflowEditorDraftController,
  edgeID: string,
  event: DragEndEvent,
): void {
  const overID = event.over?.id;
  if (overID === undefined || event.active.id === overID) {
    return;
  }
  if (typeof event.active.id !== "number" || typeof overID !== "number") {
    return;
  }
  controller.dispatch({
    activeIndex: event.active.id,
    edgeID,
    overIndex: overID,
    type: "reorderEdgeParameter",
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
          <DetailRow
            label={t("workflowEditor.assignee")}
            value={fallbackLabel(t("workflowEditor.none"), node.subagentRole)}
          />
        ) : null}
      </DetailSection>
      {node.kind === "agent" ? (
        <FieldSummary fields={derivedNode.possibleProvisionFields} title={t("workflowEditor.provides")} />
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
  const promptParameters = edgePromptPlaceholderParameters(definition, edge);
  const fanOutTransition = transitionGroupIsFanOut(definition, edge.transitionGroupID);
  return (
    <InspectorStack>
      <DetailSection
        hideTitle
        leading={
          <WorkflowEdgeRouteGraphic
            contextMode={edge.contextMode}
            hasError={details.hasErrors}
            sourceLabel={details.sourceLabel}
            targetLabel={details.targetLabel}
          />
        }
        title={t("workflowEditor.route")}
      >
        <DetailRow label={t("workflowEditor.key")} mono value={details.transitionID} />
        {fanOutTransition ? <DetailRow label={t("workflowEditor.branchKey")} mono value={edge.key} /> : null}
        <DetailRow label={t("workflowEditor.transitionGroup")} value={details.transitionGroupLabel} />
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
      {details.targetKind === "agent" ? <PromptPreview prompt={edge.promptTemplate} /> : null}
      {details.sourceKind === "agent" ? (
        <FieldSummary fields={parameterSummaryFields(edge.parameters)} title={t("workflowEditor.parameters")} />
      ) : null}
      {details.sourceKind === "join" && details.targetKind === "agent" ? (
        <FieldSummary
          fields={parameterSummaryFields(promptParameters)}
          title={t("workflowEditor.joinAggregateParameters")}
        />
      ) : null}
      {derivedEdge.inputBindings.length === 0 ? null : <Bindings bindings={derivedEdge.inputBindings} />}
      {derivedEdge.requiredProvisionFields.length === 0 ? null : (
        <FieldSummary
          fields={derivedEdge.requiredProvisionFields}
          title={t("workflowEditor.derivedProvisionRequirements")}
        />
      )}
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

function ApprovalToggle({
  checked,
  disabled = false,
  label,
  onCheckedChange,
}: Readonly<{
  checked: boolean;
  disabled?: boolean | undefined;
  label: string;
  onCheckedChange: (checked: boolean) => void;
}>) {
  const checkboxID = useId();
  const labelID = `${checkboxID}-label`;
  return (
    <div className="flex min-h-9 min-w-0 items-center gap-[var(--space-2)] rounded-[var(--radius-m)] text-sm font-semibold text-[var(--color-on-island)]">
      <Checkbox
        aria-labelledby={labelID}
        checked={checked}
        disabled={disabled}
        id={checkboxID}
        onCheckedChange={(value) => {
          if (disabled) {
            return;
          }
          onCheckedChange(value === true);
        }}
      />
      <label
        className={cx("min-w-0 select-none", disabled ? "cursor-not-allowed opacity-55" : "cursor-pointer")}
        htmlFor={checkboxID}
        id={labelID}
      >
        {label}
      </label>
    </div>
  );
}

function contextModeOptions(
  translate: Translate,
  continuationDisabled = false,
): readonly SelectFieldOption[] {
  return [
    {
      label: translate("workflowEditor.contextModeNewSession"),
      textValue: translate("workflowEditor.contextModeNewSession"),
      value: "new_session",
    },
    {
      disabled: continuationDisabled,
      label: translate("workflowEditor.contextModeContinueSession"),
      textValue: translate("workflowEditor.contextModeContinueSession"),
      value: "continue_session",
    },
    {
      disabled: continuationDisabled,
      label: translate("workflowEditor.contextModeCompactContinueSession"),
      textValue: translate("workflowEditor.contextModeCompactContinueSession"),
      value: "compact_and_continue_session",
    },
  ];
}

const immediateContextSourceOption = "__immediate_context_source__";
const missingContextSourceOption = "__missing_context_source__";
const previousTargetContextSourceOption = "__previous_target_context_source__";
const immediateContextSource: WorkflowContextSource = { kind: "immediate_source", nodeKey: "" };
const previousTargetContextSource: WorkflowContextSource = { kind: "previous_target", nodeKey: "" };

function contextSourceOptions(
  definition: WorkflowDefinition,
  edge: WorkflowEdge,
  translate: Translate,
): readonly SelectFieldOption[] {
  const validNodes = validContextSourceNodes(definition, edge);
  const nodeOptions = validNodes.map((node) => {
    const label = fallbackLabel(node.key, node.name, node.key);
    return {
      label,
      textValue: label,
      value: node.id,
    };
  });
  const options: SelectFieldOption[] = [
    {
      label: translate("workflowEditor.contextSourceImmediate"),
      textValue: translate("workflowEditor.contextSourceImmediate"),
      value: immediateContextSourceOption,
    },
    {
      disabled: !previousTargetContextSourceAvailable(definition, edge),
      label: translate("workflowEditor.contextSourcePreviousTarget"),
      textValue: translate("workflowEditor.contextSourcePreviousTarget"),
      value: previousTargetContextSourceOption,
    },
    ...nodeOptions,
  ];
  if (
    edge.contextSource.kind === "selected_node" &&
    !validNodes.some((node) => node.key === edge.contextSource.nodeKey)
  ) {
    options.push({
      disabled: true,
      label:
        edge.contextSource.nodeKey.length > 0
          ? edge.contextSource.nodeKey
          : translate("workflowEditor.contextSourceSelected"),
      textValue: edge.contextSource.nodeKey,
      value: missingContextSourceOption,
    });
  }
  return options;
}

function contextSourceSelectValue(definition: WorkflowDefinition, edge: WorkflowEdge): string {
  if (edge.contextSource.kind === "previous_target") {
    return previousTargetContextSourceOption;
  }
  if (edge.contextSource.kind !== "selected_node") {
    return immediateContextSourceOption;
  }
  return (
    validContextSourceNodes(definition, edge).find((node) => node.key === edge.contextSource.nodeKey)?.id ??
    missingContextSourceOption
  );
}

function contextSourceFromSelectValue(
  definition: WorkflowDefinition,
  value: string,
): WorkflowContextSource {
  if (value === immediateContextSourceOption) {
    return immediateContextSource;
  }
  if (value === previousTargetContextSourceOption) {
    return previousTargetContextSource;
  }
  const node = definition.nodes.find((item) => item.id === value);
  return { kind: "selected_node", nodeKey: node?.key ?? "" };
}

function previousTargetContextSourceAvailable(definition: WorkflowDefinition, edge: WorkflowEdge): boolean {
  const target = nodeByID(definition, edge.targetNodeID);
  if (target?.kind !== "agent") {
    return false;
  }
  const sourceNodeID = transitionGroupByID(definition, edge.transitionGroupID)?.sourceNodeID;
  const startNodes = definition.nodes.filter((node) => node.kind === "start");
  if (sourceNodeID === undefined || startNodes.length !== 1) {
    return true;
  }
  return nodeDominates(definition, target.id, sourceNodeID);
}

function validContextSourceNodes(definition: WorkflowDefinition, edge: WorkflowEdge): WorkflowDefinition["nodes"] {
  return definition.nodes.filter(
    (node) =>
      node.kind === "agent" &&
      node.id !== edge.targetNodeID &&
      contextSourceNodeIsGuaranteedBeforeEdgeSource(definition, edge, node.id),
  );
}

function contextSourceNodeIsGuaranteedBeforeEdgeSource(
  definition: WorkflowDefinition,
  edge: WorkflowEdge,
  nodeID: string,
): boolean {
  const sourceNodeID = transitionGroupByID(definition, edge.transitionGroupID)?.sourceNodeID;
  const startNodes = definition.nodes.filter((node) => node.kind === "start");
  if (sourceNodeID === undefined || startNodes.length !== 1) {
    return true;
  }
  return nodeDominates(definition, nodeID, sourceNodeID);
}

function nodeDominates(definition: WorkflowDefinition, candidateID: string, targetID: string): boolean {
  if (candidateID === targetID) {
    return true;
  }
  const startNodeID = definition.nodes.find((node) => node.kind === "start")?.id;
  if (startNodeID === undefined) {
    return false;
  }
  return !reachableFromSkipping(definition, startNodeID, candidateID).has(targetID);
}

function reachableFromSkipping(
  definition: WorkflowDefinition,
  startNodeID: string,
  skippedNodeID: string,
): ReadonlySet<string> {
  const visited = new Set<string>();
  const stack = startNodeID === skippedNodeID ? [] : [startNodeID];
  while (stack.length > 0) {
    const nodeID = stack.pop();
    if (nodeID === undefined || visited.has(nodeID) || nodeID === skippedNodeID) {
      continue;
    }
    visited.add(nodeID);
    for (const targetNodeID of outgoingTargetNodeIDs(definition, nodeID)) {
      if (!visited.has(targetNodeID) && targetNodeID !== skippedNodeID) {
        stack.push(targetNodeID);
      }
    }
  }
  return visited;
}

function outgoingTargetNodeIDs(definition: WorkflowDefinition, sourceNodeID: string): readonly string[] {
  const outgoingTransitionGroupIDs = new Set(
    definition.transitionGroups.filter((group) => group.sourceNodeID === sourceNodeID).map((group) => group.id),
  );
  return definition.edges
    .filter((edge) => outgoingTransitionGroupIDs.has(edge.transitionGroupID))
    .map((edge) => edge.targetNodeID);
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
      <IslandSurface as="div" className="rounded-[var(--radius-m)] p-[var(--space-2)] text-sm" level={1}>
        <MarkdownText value={prompt} />
      </IslandSurface>
    </div>
  );
}

function edgeDetails(definition: WorkflowDefinition, edge: WorkflowEdge, validation: WorkflowValidation) {
  const group = transitionGroupByID(definition, edge.transitionGroupID);
  const source = sourceNodeForTransition(definition, group);
  const target = nodeByID(definition, edge.targetNodeID);
  const errors = edgeValidationGroups(validation, edge);
  return {
    ...edgeEndpointDetails(source, target),
    ...edgeTransitionDetails(group),
    ...errors,
    hasErrors: errors.directErrors.length + errors.groupErrors.length > 0,
  };
}

function sourceNodeForTransition(
  definition: WorkflowDefinition,
  group: WorkflowTransitionGroup | undefined,
): WorkflowNode | undefined {
  return group === undefined ? undefined : nodeByID(definition, group.sourceNodeID);
}

function edgeEndpointDetails(source: WorkflowNode | undefined, target: WorkflowNode | undefined) {
  return {
    sourceKind: source?.kind ?? "",
    sourceLabel: fallbackLabel("", source?.name, source?.key),
    targetKind: target?.kind ?? "",
    targetLabel: fallbackLabel("", target?.name, target?.key),
  };
}

function edgeTransitionDetails(group: WorkflowTransitionGroup | undefined) {
  return {
    transitionGroupLabel: fallbackLabel("", group?.name, group?.id),
    transitionID: group?.transitionID ?? "",
  };
}

function edgeValidationGroups(validation: WorkflowValidation, edge: WorkflowEdge) {
  return {
    directErrors: validation.errors.filter((error) => error.edgeID === edge.id),
    groupErrors: validation.errors.filter(
      (error) => error.edgeID !== edge.id && error.transitionGroupID === edge.transitionGroupID,
    ),
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

function edgePromptPlaceholderParameters(
  definition: WorkflowDefinition,
  edge: WorkflowEdge,
): readonly WorkflowParameter[] {
  const source = edgeSourceNode(definition, edge);
  if (source?.kind === "join") {
    return derivedNodeWiring(definition, source.id).joinOutputFields.map((field) => ({
      description: field.description,
      key: field.name,
    }));
  }
  return edge.parameters;
}

function parameterSummaryFields(
  parameters: readonly WorkflowParameter[],
): readonly { name: string; description: string }[] {
  return parameters.map((parameter) => ({ description: parameter.description, name: parameter.key }));
}

function edgeSourceNode(definition: WorkflowDefinition, edge: WorkflowEdge): WorkflowNode | undefined {
  const group = transitionGroupByID(definition, edge.transitionGroupID);
  return group === undefined ? undefined : nodeByID(definition, group.sourceNodeID);
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
  if (edge.contextSource.kind === "previous_target") {
    return translate("workflowEditor.contextSourcePreviousTarget");
  }
  return translate("workflowEditor.contextSourceImmediate");
}

type Translate = ReturnType<typeof useTranslation>["t"];

const emptyWorkflowValidation: WorkflowValidation = { errors: [], valid: true };

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
