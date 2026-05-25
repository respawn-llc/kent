import {
  Background,
  BackgroundVariant,
  Handle,
  ReactFlow,
  ReactFlowProvider,
  Position,
  useReactFlow,
  type EdgeProps,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import { Info, Maximize2, Minus, Plus, RotateCcw } from "lucide-react";
import { memo, useEffect, useMemo, useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { cx } from "../../ui/classes";
import { WorkflowGraphEdge as WorkflowGraphEdgeRenderer } from "./WorkflowGraphEdge";
import type {
  WorkflowGraphEdge,
  WorkflowGraphEdgeData,
  WorkflowGraphGroupNode,
  WorkflowGraphNodeData,
  WorkflowGraphGroupData,
  WorkflowGraphLayout,
  WorkflowGraphNode,
  WorkflowGraphWorkflowNode,
} from "./workflowGraphLayout";
import "@xyflow/react/dist/style.css";
import "./workflow-editor.css";

export type WorkflowGraphCanvasProps = Readonly<{
  graph: WorkflowGraphLayout;
  onEdgeInspect: (edgeID: string) => void;
  onGroupInspect: (groupID: string) => void;
  onNodeInspect: (nodeID: string) => void;
  onWorkflowInspect: () => void;
}>;

export function WorkflowGraphCanvas({
  graph,
  onEdgeInspect,
  onGroupInspect,
  onNodeInspect,
  onWorkflowInspect,
}: WorkflowGraphCanvasProps) {
  const localNodeTypes = useMemo(
    () => ({
      workflowGroup: WorkflowGroupNode,
      workflowJoin: WorkflowJoinNode,
      workflowNode: WorkflowNode,
    }),
    [],
  );
  const localEdgeTypes = useMemo(
    () => ({
      workflow: (props: EdgeProps<WorkflowGraphEdge>) => (
        <WorkflowGraphEdgeRenderer
          {...props}
          onInspect={(edgeID) => {
            onEdgeInspect(edgeID);
          }}
        />
      ),
    }),
    [onEdgeInspect],
  );
  return (
    <ReactFlowProvider>
      <WorkflowGraphCanvasInner
        edgeTypes={localEdgeTypes}
        edges={graph.edges}
        onEdgeInspect={onEdgeInspect}
        onGroupInspect={onGroupInspect}
        onNodeInspect={onNodeInspect}
        onWorkflowInspect={onWorkflowInspect}
        nodeTypes={localNodeTypes}
        nodes={graph.nodes}
      />
    </ReactFlowProvider>
  );
}

function WorkflowGraphCanvasInner({
  edgeTypes,
  edges,
  onEdgeInspect,
  onGroupInspect,
  onNodeInspect,
  onWorkflowInspect,
  nodeTypes,
  nodes,
}: Readonly<{
  edgeTypes: Readonly<Record<string, (props: EdgeProps<WorkflowGraphEdge>) => ReactNode>>;
  edges: readonly WorkflowGraphEdge[];
  onEdgeInspect: (edgeID: string) => void;
  onGroupInspect: (groupID: string) => void;
  onNodeInspect: (nodeID: string) => void;
  onWorkflowInspect: () => void;
  nodeTypes: Readonly<Record<string, typeof WorkflowGroupNode | typeof WorkflowJoinNode | typeof WorkflowNode>>;
  nodes: readonly WorkflowGraphNode[];
}>) {
  const { t } = useTranslation();
  const instance = useReactFlow();
  const didFitInitialView = useRef(false);
  useEffect(() => {
    if (didFitInitialView.current) {
      return;
    }
    didFitInitialView.current = true;
    window.requestAnimationFrame(() => {
      void instance.fitView({ padding: 0.18 });
    });
  }, [instance]);
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent): void {
      if (event.defaultPrevented || isFormTarget(event.target)) {
        return;
      }
      if (event.key === "+") {
        event.preventDefault();
        void instance.zoomIn();
      } else if (event.key === "-") {
        event.preventDefault();
        void instance.zoomOut();
      } else if (event.key === "0") {
        event.preventDefault();
        void instance.setViewport({ x: 0, y: 0, zoom: 1 });
      } else if (event.key.toLowerCase() === "f") {
        event.preventDefault();
        void instance.fitView({ padding: 0.18 });
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [instance]);

  return (
    <div className="workflow-editor-canvas h-full min-h-0 w-full" data-testid="workflow-editor-canvas">
      <ReactFlow
        colorMode="system"
        defaultEdges={edges}
        defaultNodes={nodes}
        edges={edges}
        edgeTypes={edgeTypes}
        fitView
        maxZoom={2}
        minZoom={0.15}
        nodes={nodes}
        nodesConnectable={false}
        nodesDraggable={false}
        nodeTypes={nodeTypes}
        onEdgeClick={(_event, edge) => {
          inspectEdge(edge, onEdgeInspect);
        }}
        onNodeClick={(_event, node) => {
          inspectNode(node, onGroupInspect, onNodeInspect);
        }}
        panOnScroll
        proOptions={{ hideAttribution: true }}
        selectionOnDrag={false}
        zoomOnDoubleClick={false}
      >
        <Background
          bgColor="transparent"
          color="var(--color-outline)"
          gap={24}
          size={1}
          variant={BackgroundVariant.Dots}
        />
        <div className="workflow-editor-tools island-glass app-region-no-drag absolute left-[var(--space-4)] top-[var(--space-4)] z-10 grid gap-[var(--space-1)] rounded-[var(--radius-l)] border p-[var(--space-1)] shadow-[var(--shadow-island-1)]">
          <CanvasTool label={t("workflowEditor.inspectWorkflow")} onClick={onWorkflowInspect}>
            <Info aria-hidden="true" size={18} strokeWidth={1.7} />
          </CanvasTool>
          <CanvasTool label={t("workflowEditor.zoomIn")} onClick={() => void instance.zoomIn()}>
            <Plus aria-hidden="true" size={18} strokeWidth={1.7} />
          </CanvasTool>
          <CanvasTool label={t("workflowEditor.zoomOut")} onClick={() => void instance.zoomOut()}>
            <Minus aria-hidden="true" size={18} strokeWidth={1.7} />
          </CanvasTool>
          <CanvasTool
            label={t("workflowEditor.fitView")}
            onClick={() => void instance.fitView({ padding: 0.18 })}
          >
            <Maximize2 aria-hidden="true" size={18} strokeWidth={1.7} />
          </CanvasTool>
          <CanvasTool
            label={t("workflowEditor.resetZoom")}
            onClick={() => void instance.setViewport({ x: 0, y: 0, zoom: 1 })}
          >
            <RotateCcw aria-hidden="true" size={18} strokeWidth={1.7} />
          </CanvasTool>
        </div>
      </ReactFlow>
    </div>
  );
}

function CanvasTool({
  children,
  label,
  onClick,
}: Readonly<{ children: ReactNode; label: string; onClick: () => void }>) {
  return (
    <button
      aria-label={label}
      className="grid size-9 place-items-center rounded-[var(--radius-m)] border border-transparent bg-transparent text-[var(--color-on-island)] transition-colors hover:bg-[var(--color-island-1)] focus-visible:border-[var(--color-primary)] focus-visible:outline-none"
      onClick={onClick}
      title={label}
      type="button"
    >
      {children}
    </button>
  );
}

const WorkflowNode = memo(function WorkflowNode({ data, selected }: NodeProps<WorkflowGraphWorkflowNode>) {
  return (
    <div
      className={cx(
        "workflow-editor-node grid h-full min-w-0 grid-rows-[minmax(0,1fr)_auto] rounded-[var(--radius-l)] border bg-[var(--color-island-1)] p-[var(--space-3)] shadow-[var(--shadow-island-1)]",
        data.hasError ? "workflow-editor-node-error" : "border-[var(--color-outline)]",
        selected ? "workflow-editor-node-selected" : undefined,
      )}
      data-kind={data.kind}
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
      <strong className="line-clamp-2 min-w-0 text-[0.95rem] leading-snug text-[var(--color-on-island)]">
        {data.label}
      </strong>
      <span className="min-w-0 truncate font-mono text-sm text-[var(--color-muted)]">{data.role}</span>
    </div>
  );
});

const WorkflowGroupNode = memo(function WorkflowGroupNode({ data }: NodeProps<WorkflowGraphGroupNode>) {
  const { t } = useTranslation();
  return (
    <div
      className={cx(
        "workflow-editor-group h-full rounded-[var(--radius-xl)] border border-[var(--color-outline)] bg-[color-mix(in_srgb,var(--color-island-1)_58%,transparent)] p-[var(--space-3)]",
        data.hasError ? "workflow-editor-node-error" : undefined,
      )}
    >
      <div className="font-mono text-xs font-bold uppercase tracking-[0.16em] text-[var(--color-muted)]">
        {data.label}
      </div>
      {"empty" in data && data.empty ? (
        <div className="grid h-[calc(100%-24px)] place-items-center text-sm text-[var(--color-muted)]">
          {t("workflowEditor.emptyGroup")}
        </div>
      ) : null}
    </div>
  );
});

const WorkflowJoinNode = memo(function WorkflowJoinNode({ data, selected }: NodeProps<WorkflowGraphWorkflowNode>) {
  return (
    <div
      className={cx(
        "workflow-editor-join-node grid h-full w-full place-items-center",
        data.hasError ? "workflow-editor-node-error" : undefined,
        selected ? "workflow-editor-node-selected" : undefined,
      )}
      data-kind={data.kind}
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
      <div className="workflow-editor-join-diamond" data-testid="workflow-join-diamond">
        <span className="sr-only">{data.label}</span>
      </div>
    </div>
  );
});

function inspectNode(
  node: Node,
  onGroupInspect: (groupID: string) => void,
  onNodeInspect: (nodeID: string) => void,
): void {
  const { data } = node;
  if (isWorkflowGraphGroupData(data)) {
    onGroupInspect(data.entityID);
    return;
  }
  if (isWorkflowGraphNodeData(data)) {
    onNodeInspect(data.entityID);
  }
}

function inspectEdge(edge: Edge, onEdgeInspect: (edgeID: string) => void): void {
  const { data } = edge;
  if (isWorkflowGraphEdgeData(data)) {
    onEdgeInspect(data.entityID);
  }
}

function isWorkflowGraphNodeData(data: Node["data"]): data is WorkflowGraphNodeData {
  return data.entityKind === "node" && typeof data.entityID === "string";
}

function isWorkflowGraphGroupData(data: Node["data"]): data is WorkflowGraphGroupData {
  return data.entityKind === "group" && typeof data.entityID === "string";
}

function isWorkflowGraphEdgeData(data: Edge["data"]): data is WorkflowGraphEdgeData {
  return data?.entityKind === "edge" && typeof data.entityID === "string";
}

function isFormTarget(target: EventTarget | null): boolean {
  return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement;
}
