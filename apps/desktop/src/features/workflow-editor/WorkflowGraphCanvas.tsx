import {
  Background,
  BackgroundVariant,
  BaseEdge,
  EdgeLabelRenderer,
  Handle,
  ReactFlow,
  ReactFlowProvider,
  Position,
  useReactFlow,
  type EdgeProps,
  type NodeProps,
} from "@xyflow/react";
import { Maximize2, Minus, Plus, RotateCcw } from "lucide-react";
import { memo, useEffect, useMemo, useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { cx } from "../../ui/classes";
import type {
  WorkflowGraphEdge,
  WorkflowGraphGroupNode,
  WorkflowGraphLayout,
  WorkflowGraphNode,
  WorkflowGraphWorkflowNode,
} from "./workflowGraphLayout";
import { workflowEdgePath } from "./workflowEdgePath";
import "@xyflow/react/dist/style.css";
import "./workflow-editor.css";

export type WorkflowGraphCanvasProps = Readonly<{
  graph: WorkflowGraphLayout;
}>;

export function WorkflowGraphCanvas({ graph }: WorkflowGraphCanvasProps) {
  const localNodeTypes = useMemo(
    () => ({
      workflowGroup: WorkflowGroupNode,
      workflowNode: WorkflowNode,
    }),
    [],
  );
  const localEdgeTypes = useMemo(
    () => ({
      workflow: WorkflowEdge,
    }),
    [],
  );
  return (
    <ReactFlowProvider>
      <WorkflowGraphCanvasInner
        edgeTypes={localEdgeTypes}
        edges={graph.edges}
        nodeTypes={localNodeTypes}
        nodes={graph.nodes}
      />
    </ReactFlowProvider>
  );
}

function WorkflowGraphCanvasInner({
  edgeTypes,
  edges,
  nodeTypes,
  nodes,
}: Readonly<{
  edgeTypes: Readonly<Record<string, typeof WorkflowEdge>>;
  edges: readonly WorkflowGraphEdge[];
  nodeTypes: Readonly<Record<string, typeof WorkflowGroupNode | typeof WorkflowNode>>;
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

function WorkflowEdge(props: EdgeProps<WorkflowGraphEdge>) {
  const edgePath = workflowEdgePath(props);
  const label = props.data?.label ?? "";
  return (
    <>
      <BaseEdge
        {...(props.markerEnd === undefined ? {} : { markerEnd: props.markerEnd })}
        data-testid="workflow-edge-path"
        path={edgePath.path}
        style={{
          stroke: props.data?.hasError === true ? "var(--color-error)" : "var(--color-outline)",
          strokeLinecap: "round",
          strokeLinejoin: "round",
          strokeWidth: props.selected ? 2.5 : 1.5,
        }}
      />
      {label.length > 0 ? (
        <EdgeLabelRenderer>
          <div
            className={cx(
              "workflow-editor-edge-label absolute max-w-[180px] truncate rounded-full border bg-[var(--color-island-1)] px-[var(--space-2)] py-[2px] text-xs font-semibold text-[var(--color-on-island)] shadow-[var(--shadow-island-1)]",
              props.data?.hasError === true ? "border-[var(--color-error)]" : "border-[var(--color-outline)]",
            )}
            style={{
              transform: `translate(-50%, -50%) translate(${edgePath.labelPoint.x.toString()}px, ${edgePath.labelPoint.y.toString()}px)`,
            }}
            title={label}
          >
            {label}
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}

function isFormTarget(target: EventTarget | null): boolean {
  return target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement;
}
