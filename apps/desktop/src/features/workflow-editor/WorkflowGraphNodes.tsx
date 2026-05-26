import { Handle, Position, type NodeProps } from "@xyflow/react";
import { memo, type CSSProperties, type MouseEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
  IslandSurface,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "../../ui";
import { cx } from "../../ui/classes";
import {
  WorkflowNodeInfoTooltipContent,
  type CopyText,
} from "./WorkflowGraphNodeMetadata";
import type { WorkflowGraphSelection } from "./workflowGraphSelection";
import type {
  WorkflowGraphGroupNode,
  WorkflowGraphNodeData,
  WorkflowGraphWorkflowNode,
} from "./workflowGraphLayout";

export type { CopyText } from "./WorkflowGraphNodeMetadata";

export type WorkflowGroupDragState = Readonly<{ label: string; nodeID: string; x: number; y: number }>;

type WorkflowNodeContextMenuCallbacks = Readonly<{
  onCreateNodeGroup: ((nodeID: string) => void) | undefined;
  onDeleteSelection: ((selection: WorkflowGraphSelection) => void) | undefined;
  onRemoveNodeFromGroup: ((nodeID: string) => void) | undefined;
  onSelectContextMenu: (nodeID: string) => void;
}>;

export function WorkflowGroupDragPreview({
  drag,
}: Readonly<{ drag: WorkflowGroupDragState }>) {
  return (
    <IslandSurface
      as="div"
      className="pointer-events-none fixed z-50 rounded-[var(--radius-m)] px-[var(--space-3)] py-[var(--space-2)] text-sm font-semibold text-[var(--color-on-island)]"
      level={2}
      style={{
        borderColor: "var(--color-primary)",
        left: drag.x + 10,
        top: drag.y + 10,
      }}
    >
      {drag.label}
    </IslandSurface>
  );
}

function WorkflowNodeContextMenuShell({
  children,
  data,
  onCreateNodeGroup,
  onDeleteSelection,
  onRemoveNodeFromGroup,
  onSelectContextMenu,
  tooltip,
}: Readonly<
  {
    children: ReactNode;
    data: WorkflowGraphNodeData;
    tooltip?: ReactNode | undefined;
  } & WorkflowNodeContextMenuCallbacks
>) {
  const { t } = useTranslation();
  const trigger = (
    <ContextMenuTrigger
      asChild
      onContextMenu={() => {
        onSelectContextMenu(data.entityID);
      }}
    >
      {tooltip === undefined ? children : <TooltipTrigger asChild>{children}</TooltipTrigger>}
    </ContextMenuTrigger>
  );
  return (
    <ContextMenu>
      {tooltip === undefined ? (
        trigger
      ) : (
        <Tooltip>
          {trigger}
          <TooltipContent
            className={NODE_METADATA_TOOLTIP_CLASS}
            data-testid="workflow-node-metadata-tooltip"
            onClick={stopPropagation}
          >
            {tooltip}
          </TooltipContent>
        </Tooltip>
      )}
      <ContextMenuContent>
        {data.kind === "agent" && data.groupID.length === 0 ? (
          <ContextMenuItem
            onSelect={() => {
              onCreateNodeGroup?.(data.entityID);
            }}
          >
            {t("workflowEditor.createNodeGroup")}
          </ContextMenuItem>
        ) : null}
        {data.kind === "agent" && data.groupID.length > 0 ? (
          <ContextMenuItem
            onSelect={() => {
              onRemoveNodeFromGroup?.(data.entityID);
            }}
          >
            {t("workflowEditor.ungroupNode")}
          </ContextMenuItem>
        ) : null}
        <ContextMenuItem
          onSelect={() => {
            onDeleteSelection?.({ kind: "node", nodeID: data.entityID });
          }}
        >
          {t("workflowEditor.deleteNode")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

const NODE_METADATA_TOOLTIP_CLASS =
  "pointer-events-auto grid w-[420px] max-w-[calc(100vw-var(--space-4)*2)] items-stretch gap-1.5 p-1.5";

export const WorkflowNode = memo(function WorkflowNode({
  data,
  onCopyText,
  onCreateNodeGroup,
  onDeleteSelection,
  onRemoveNodeFromGroup,
  onSelectContextMenu,
  onStartGroupDrag,
  selected,
}: NodeProps<WorkflowGraphWorkflowNode> &
  Readonly<
    {
      onCopyText: CopyText;
      onStartGroupDrag: (drag: WorkflowGroupDragState) => void;
    } & WorkflowNodeContextMenuCallbacks
  >) {
  const { t } = useTranslation();
  const nodeCard = (
    <IslandSurface
      as="div"
      className={cx(
        "workflow-editor-node relative grid h-full min-w-0 grid-rows-[minmax(0,1fr)_auto] rounded-[var(--radius-l)] p-[var(--space-3)]",
        data.hasError ? "workflow-editor-node-error" : undefined,
        selected ? "workflow-editor-node-selected" : undefined,
      )}
      data-kind={data.kind}
      data-testid={`workflow-graph-node-${data.entityID}`}
      level={3}
      style={workflowNodeOutlineStyle(data.kind, data.hasError)}
    >
      <Handle
        aria-label="Incoming transitions"
        className="workflow-editor-handle"
        data-testid="workflow-node-target-handle"
        position={Position.Left}
        type="target"
      />
      {data.kind === "terminal" ? null : (
        <Handle
          aria-label="Outgoing transitions"
          className="workflow-editor-handle"
          data-testid="workflow-node-source-handle"
          position={Position.Right}
          type="source"
        />
      )}
      <strong className="line-clamp-2 min-w-0 text-[0.95rem] leading-snug text-[var(--color-on-island)]">
        {data.label}
      </strong>
      {data.kind === "agent" ? (
        <button
          aria-label={t("workflowEditor.dragNodeToGroup")}
          className="absolute right-[var(--space-2)] top-[var(--space-2)] rounded-full border border-[var(--color-outline)] bg-[var(--color-island-2)] px-[var(--space-2)] py-[2px] text-[0.65rem] font-bold uppercase tracking-[0.12em] text-[var(--color-muted)]"
          onPointerDown={(event) => {
            event.preventDefault();
            event.stopPropagation();
            onStartGroupDrag({ label: data.label, nodeID: data.entityID, x: event.clientX, y: event.clientY });
          }}
          type="button"
        >
          {t("workflowEditor.groupDragHandle")}
        </button>
      ) : null}
      <span className="min-w-0 truncate font-mono text-sm text-[var(--color-muted)]">{data.role}</span>
    </IslandSurface>
  );
  const tooltip = usesCompactNodeTooltip(data.kind) ? (
    <WorkflowNodeInfoTooltipContent
      nodeID={data.entityID}
      nodeKey={data.key}
      onCopyText={onCopyText}
    />
  ) : undefined;
  return (
    <WorkflowNodeContextMenuShell
      data={data}
      onCreateNodeGroup={onCreateNodeGroup}
      onDeleteSelection={onDeleteSelection}
      onRemoveNodeFromGroup={onRemoveNodeFromGroup}
      onSelectContextMenu={onSelectContextMenu}
      tooltip={tooltip}
    >
      {nodeCard}
    </WorkflowNodeContextMenuShell>
  );
});

export const WorkflowGroupNode = memo(function WorkflowGroupNode({ data }: NodeProps<WorkflowGraphGroupNode>) {
  const { t } = useTranslation();
  return (
    <IslandSurface
      as="div"
      className={cx(
        "workflow-editor-group h-full rounded-[var(--radius-xl)] p-[var(--space-3)]",
        data.hasError ? "workflow-editor-node-error" : undefined,
      )}
      data-testid={`workflow-graph-group-${data.entityID}`}
      data-workflow-group-id={data.entityID}
      level={1}
      style={workflowNodeOutlineStyle(data.kind, data.hasError)}
    >
      <div className="font-mono text-xs font-bold uppercase tracking-[0.16em] text-[var(--color-muted)]">
        {data.label}
      </div>
      {"empty" in data && data.empty ? (
        <div className="grid h-[calc(100%-24px)] place-items-center text-sm text-[var(--color-muted)]">
          {t("workflowEditor.emptyGroup")}
        </div>
      ) : null}
    </IslandSurface>
  );
});

export const WorkflowJoinNode = memo(function WorkflowJoinNode({
  data,
  onCopyText,
  onDeleteSelection,
  onSelectContextMenu,
  selected,
}: NodeProps<WorkflowGraphWorkflowNode> &
  Readonly<
    {
      onCopyText: CopyText;
    } & Pick<WorkflowNodeContextMenuCallbacks, "onDeleteSelection" | "onSelectContextMenu">
  >) {
  const nodeCard = (
    <div
      className={cx(
        "workflow-editor-join-node grid h-full w-full place-items-center",
        data.hasError ? "workflow-editor-node-error" : undefined,
      )}
      style={workflowNodeOutlineStyle(data.kind, data.hasError)}
      title={data.label}
    >
      <Handle
        aria-label="Incoming transitions"
        className="workflow-editor-handle"
        data-testid="workflow-node-target-handle"
        position={Position.Left}
        type="target"
      />
      <Handle
        aria-label="Outgoing transitions"
        className="workflow-editor-handle"
        data-testid="workflow-node-source-handle"
        position={Position.Right}
        type="source"
      />
      <IslandSurface
        as="div"
        className={cx("workflow-editor-join-diamond", selected ? "workflow-editor-node-selected" : undefined)}
        data-kind={data.kind}
        data-testid={`workflow-graph-node-${data.entityID}`}
        level={3}
        style={workflowNodeOutlineStyle(data.kind, data.hasError)}
      >
        <span className="sr-only">{data.label}</span>
      </IslandSurface>
    </div>
  );
  const tooltip = (
    <WorkflowNodeInfoTooltipContent
      nodeID={data.entityID}
      nodeKey={data.key}
      onCopyText={onCopyText}
    />
  );
  return (
    <WorkflowNodeContextMenuShell
      data={data}
      onCreateNodeGroup={undefined}
      onDeleteSelection={onDeleteSelection}
      onRemoveNodeFromGroup={undefined}
      onSelectContextMenu={onSelectContextMenu}
      tooltip={tooltip}
    >
      {nodeCard}
    </WorkflowNodeContextMenuShell>
  );
});

type WorkflowNodeOutlineStyle = CSSProperties &
  Readonly<Record<"--workflow-editor-node-outline-color", string>>;

function workflowNodeOutlineStyle(kind: string, hasError: boolean): WorkflowNodeOutlineStyle {
  if (hasError) {
    return { "--workflow-editor-node-outline-color": "var(--color-error)" };
  }
  if (kind === "start") {
    return { "--workflow-editor-node-outline-color": "var(--color-primary)" };
  }
  if (kind === "terminal") {
    return { "--workflow-editor-node-outline-color": "var(--color-success)" };
  }
  if (kind === "join") {
    return { "--workflow-editor-node-outline-color": "var(--color-secondary)" };
  }
  return { "--workflow-editor-node-outline-color": "var(--color-outline)" };
}

function usesCompactNodeTooltip(kind: string): boolean {
  return !isEditableWorkflowNodeKind(kind);
}

function isEditableWorkflowNodeKind(kind: string): boolean {
  return kind === "agent" || kind === "join";
}

function stopPropagation(event: MouseEvent): void {
  event.stopPropagation();
}
