import {
  Background,
  BackgroundVariant,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type EdgeProps,
  type NodeProps,
  type NodeTypes,
} from "@xyflow/react";
import { useEffect, useMemo, useRef, useState } from "react";

import { TooltipProvider } from "../../ui";
import {
  connectWorkflowGraphNodes,
  inspectEdge,
  inspectNode,
  isFormTarget,
  selectionFromEdge,
  selectionFromNode,
  workflowGraphSelectionExists,
} from "./workflowGraphCanvasInteractions";
import { WorkflowGraphEdge as WorkflowGraphEdgeRenderer } from "./WorkflowGraphEdge";
import {
  WorkflowGroupNode,
  WorkflowJoinNode,
  WorkflowNode,
} from "./WorkflowGraphNodes";
import type { CopyText } from "./WorkflowGraphNodeMetadata";
import { WorkflowGraphToolbar } from "./WorkflowGraphToolbar";
import type { WorkflowGraphSelection } from "./workflowGraphSelection";
import type {
  WorkflowGraphEdge,
  WorkflowGraphGroupNode,
  WorkflowGraphLayout,
  WorkflowGraphNode,
  WorkflowGraphWorkflowNode,
} from "./workflowGraphLayout";
import "@xyflow/react/dist/style.css";
import "./workflow-editor.css";

export { WorkflowNodeInfoTooltipContent } from "./WorkflowGraphNodeMetadata";

export type WorkflowGraphCanvasProps = Readonly<{
  graph: WorkflowGraphLayout;
  onCopyText?: ((value: string) => Promise<void> | void) | undefined;
  onAddNode?: ((kind: "agent" | "terminal") => void) | undefined;
  onAddNodeToGroup?: ((nodeID: string, groupID: string) => void) | undefined;
  onConnectNodes?: ((sourceNodeID: string, targetNodeID: string) => void) | undefined;
  onCreateNodeGroup?: ((nodeID: string) => void) | undefined;
  onDeleteSelection?: ((selection: WorkflowGraphSelection) => void) | undefined;
  onRemoveNodeFromGroup?: ((nodeID: string) => void) | undefined;
  onEdgeInspect: (edgeID: string) => void;
  onGroupInspect: (groupID: string) => void;
  onNodeInspect: (nodeID: string) => void;
  onWorkflowInspect: () => void;
}>;

export function WorkflowGraphCanvas({
  graph,
  onCopyText = copyTextWithNavigator,
  onAddNode,
  onAddNodeToGroup,
  onConnectNodes,
  onCreateNodeGroup,
  onDeleteSelection,
  onRemoveNodeFromGroup,
  onEdgeInspect,
  onGroupInspect,
  onNodeInspect,
  onWorkflowInspect,
}: WorkflowGraphCanvasProps) {
  return (
    <TooltipProvider delayDuration={0}>
      <ReactFlowProvider>
        <WorkflowGraphCanvasInner
          edges={graph.edges}
          onAddNode={onAddNode}
          onAddNodeToGroup={onAddNodeToGroup}
          onConnectNodes={onConnectNodes}
          onCreateNodeGroup={onCreateNodeGroup}
          onDeleteSelection={onDeleteSelection}
          onRemoveNodeFromGroup={onRemoveNodeFromGroup}
          onEdgeInspect={onEdgeInspect}
          onGroupInspect={onGroupInspect}
          onNodeInspect={onNodeInspect}
          onWorkflowInspect={onWorkflowInspect}
          onCopyText={onCopyText}
          nodes={graph.nodes}
        />
      </ReactFlowProvider>
    </TooltipProvider>
  );
}

function WorkflowGraphCanvasInner({
  edges,
  onAddNode,
  onAddNodeToGroup,
  onConnectNodes,
  onCopyText,
  onCreateNodeGroup,
  onDeleteSelection,
  onEdgeInspect,
  onGroupInspect,
  onNodeInspect,
  onRemoveNodeFromGroup,
  onWorkflowInspect,
  nodes,
}: Readonly<{
  edges: readonly WorkflowGraphEdge[];
  onAddNode: ((kind: "agent" | "terminal") => void) | undefined;
  onAddNodeToGroup: ((nodeID: string, groupID: string) => void) | undefined;
  onConnectNodes: ((sourceNodeID: string, targetNodeID: string) => void) | undefined;
  onCopyText: CopyText;
  onCreateNodeGroup: ((nodeID: string) => void) | undefined;
  onDeleteSelection: ((selection: WorkflowGraphSelection) => void) | undefined;
  onEdgeInspect: (edgeID: string) => void;
  onGroupInspect: (groupID: string) => void;
  onNodeInspect: (nodeID: string) => void;
  onRemoveNodeFromGroup: ((nodeID: string) => void) | undefined;
  onWorkflowInspect: () => void;
  nodes: readonly WorkflowGraphNode[];
}>) {
  const instance = useReactFlow();
  const [selection, setSelection] = useState<WorkflowGraphSelection | null>(null);
  const edgeTypes = useMemo(
    () => ({
      workflow: (props: EdgeProps<WorkflowGraphEdge>) => (
        <WorkflowGraphEdgeRenderer
          {...props}
          onInspect={(edgeID) => {
            setSelection({ edgeID, kind: "edge" });
            onEdgeInspect(edgeID);
          }}
        />
      ),
    }),
    [onEdgeInspect],
  );
  const nodeTypes = useMemo(
    () => ({
      workflowGroup: (props: NodeProps<WorkflowGraphGroupNode>) => (
        <WorkflowGroupNode {...props} onAddNodeToGroup={onAddNodeToGroup} />
      ),
      workflowJoin: (props: NodeProps<WorkflowGraphWorkflowNode>) => (
        <WorkflowJoinNode
          {...props}
          onCopyText={onCopyText}
          onDeleteSelection={onDeleteSelection}
          onSelectContextMenu={(nodeID) => {
            setSelection({ kind: "node", nodeID });
          }}
        />
      ),
      workflowNode: (props: NodeProps<WorkflowGraphWorkflowNode>) => (
        <WorkflowNode
          {...props}
          onAddNodeToGroup={onAddNodeToGroup}
          onCopyText={onCopyText}
          onCreateNodeGroup={onCreateNodeGroup}
          onDeleteSelection={onDeleteSelection}
          onRemoveNodeFromGroup={onRemoveNodeFromGroup}
          onSelectContextMenu={(nodeID) => {
            setSelection({ kind: "node", nodeID });
          }}
        />
      ),
    }) satisfies NodeTypes,
    [onAddNodeToGroup, onCopyText, onCreateNodeGroup, onDeleteSelection, onRemoveNodeFromGroup],
  );
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
      const activeSelection =
        selection === null || !workflowGraphSelectionExists(selection, nodes, edges) ? null : selection;
      if (event.defaultPrevented || isFormTarget(event.target)) {
        return;
      }
      if (applyViewportShortcut(event.key, instance)) {
        event.preventDefault();
        return;
      }
      if ((event.key === "Delete" || event.key === "Backspace") && activeSelection !== null) {
        event.preventDefault();
        onDeleteSelection?.(activeSelection);
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [edges, instance, nodes, onDeleteSelection, selection]);
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
        nodesConnectable={onConnectNodes !== undefined}
        nodesDraggable={false}
        nodeTypes={nodeTypes}
        onConnect={(connection) => {
          connectWorkflowGraphNodes(connection, onConnectNodes);
        }}
        onEdgeClick={(_event, edge) => {
          setSelection(selectionFromEdge(edge));
          inspectEdge(edge, onEdgeInspect);
        }}
        onNodeClick={(_event, node) => {
          setSelection(selectionFromNode(node));
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
        <WorkflowGraphToolbar onAddNode={onAddNode} onWorkflowInspect={onWorkflowInspect} />
      </ReactFlow>
    </div>
  );
}

function copyTextWithNavigator(value: string): void {
  void navigator.clipboard.writeText(value).catch(() => undefined);
}

function applyViewportShortcut(key: string, instance: ReturnType<typeof useReactFlow>): boolean {
  if (key === "+") {
    void instance.zoomIn();
    return true;
  }
  if (key === "-") {
    void instance.zoomOut();
    return true;
  }
  if (key === "0") {
    void instance.setViewport({ x: 0, y: 0, zoom: 1 });
    return true;
  }
  if (key.toLowerCase() === "f") {
    void instance.fitView({ padding: 0.18 });
    return true;
  }
  return false;
}
