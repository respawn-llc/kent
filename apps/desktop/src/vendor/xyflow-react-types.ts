import type { ComponentType, CSSProperties, ReactNode } from "react";
import type { MouseEvent } from "react";

// Version-locked lightweight declaration shim for @xyflow/react 12.10.x.
// On @xyflow/react upgrades, verify the node-drag API surface below against upstream before bumping.
export type XYPosition = Readonly<{ x: number; y: number }>;

export type PositionValue = "left" | "right" | "top" | "bottom";

export type Node<Data extends Record<string, unknown> = Record<string, unknown>> = Readonly<{
  id: string;
  type?: string;
  parentId?: string;
  extent?: "parent";
  position: XYPosition;
  sourcePosition?: PositionValue;
  targetPosition?: PositionValue;
  data: Data;
  draggable?: boolean;
  selectable?: boolean;
  selected?: boolean;
  style?: CSSProperties;
}>;

export type NodeChange<NodeType extends Node = Node> = Readonly<{
  dragging?: boolean;
  id: string;
  item?: NodeType;
  position?: XYPosition;
  selected?: boolean;
  type: string;
}>;

export type Edge<Data extends Record<string, unknown> = Record<string, unknown>> = Readonly<{
  id: string;
  source: string;
  target: string;
  type?: string;
  data?: Data;
  markerEnd?: string | Readonly<{ color?: string; type: string }>;
  selected?: boolean;
}>;

export type Connection = Readonly<{
  source: string | null;
  target: string | null;
}>;

export type NodeProps<NodeType extends Node = Node> = Readonly<{
  data: NodeType["data"];
  dragging?: boolean;
  selected: boolean;
}>;

export type EdgeProps<EdgeType extends Edge = Edge> = EdgeType &
  Readonly<{
    sourceX: number;
    sourceY: number;
    targetX: number;
    targetY: number;
    sourcePosition?: string;
    targetPosition?: string;
  }>;

export type NodeTypes = unknown;
export type EdgeTypes = unknown;

export declare const ReactFlow: ComponentType<
  Readonly<{
    children?: ReactNode;
    colorMode?: string;
    defaultEdges?: readonly Edge[];
    defaultNodes?: readonly Node[];
    edges?: readonly Edge[];
    edgeTypes?: EdgeTypes;
    fitView?: boolean;
    maxZoom?: number;
    minZoom?: number;
    nodes?: readonly Node[];
    nodesConnectable?: boolean;
    nodesDraggable?: boolean;
    nodeDragThreshold?: number;
    nodeTypes?: NodeTypes;
    onConnect?: (connection: Connection) => void;
    onEdgeClick?: (event: unknown, edge: Edge) => void;
    onNodeClick?: (event: unknown, node: Node) => void;
    onNodeContextMenu?: (event: MouseEvent, node: Node) => void;
    onNodeDrag?: (event: MouseEvent, node: Node, nodes: readonly Node[]) => void;
    onNodeDragStart?: (event: MouseEvent, node: Node, nodes: readonly Node[]) => void;
    onNodeDragStop?: (event: MouseEvent, node: Node, nodes: readonly Node[]) => void;
    onNodesChange?: (changes: readonly NodeChange[]) => void;
    panOnScroll?: boolean;
    proOptions?: Readonly<{ hideAttribution?: boolean }>;
    selectionOnDrag?: boolean;
    zoomOnDoubleClick?: boolean;
  }>
>;

export declare const ReactFlowProvider: ComponentType<Readonly<{ children?: ReactNode }>>;
export declare const Background: ComponentType<Readonly<{ bgColor?: string; color?: string; gap?: number; size?: number; variant?: string }>>;
export declare const BackgroundVariant: Readonly<{ Dots: string }>;
export declare const BaseEdge: ComponentType<Readonly<{ "data-testid"?: string; markerEnd?: string | Readonly<{ color?: string; type: string }>; path: string; style?: CSSProperties }>>;
export declare const EdgeLabelRenderer: ComponentType<Readonly<{ children?: ReactNode }>>;
export declare const Handle: ComponentType<
  Readonly<{
    "aria-label"?: string;
    className?: string;
    "data-testid"?: string;
    onClick?: (event: MouseEvent) => void;
    position: PositionValue;
    type: "source" | "target";
  }>
>;
export declare const MarkerType: Readonly<{ ArrowClosed: string }>;
export declare const Position: Readonly<{ Bottom: "bottom"; Left: "left"; Right: "right"; Top: "top" }>;

export declare function getBezierPath(props: EdgeProps): [string, number, number];

export declare function applyNodeChanges<NodeType extends Node>(
  changes: readonly NodeChange<NodeType>[],
  nodes: readonly NodeType[],
): NodeType[];

export declare function useReactFlow(): Readonly<{
  fitView(options?: Readonly<{ padding?: number }>): Promise<boolean>;
  setViewport(viewport: Readonly<{ x: number; y: number; zoom: number }>): Promise<boolean>;
  zoomIn(): Promise<boolean>;
  zoomOut(): Promise<boolean>;
}>;
