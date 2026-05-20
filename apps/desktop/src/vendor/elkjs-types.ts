export type LayoutOptions = Readonly<Record<string, string>>;

export type ElkPoint = Readonly<{
  x: number;
  y: number;
}>;

export type ElkShape = Readonly<{
  id?: string;
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  layoutOptions?: LayoutOptions;
}>;

export type ElkExtendedEdge = ElkShape &
  Readonly<{
    id: string;
    sources: readonly string[];
    targets: readonly string[];
    sections?: readonly ElkEdgeSection[];
  }>;

export type ElkEdgeSection = ElkShape &
  Readonly<{
    id: string;
    startPoint: ElkPoint;
    endPoint: ElkPoint;
    bendPoints?: readonly ElkPoint[];
  }>;

export type ElkNode = ElkShape &
  Readonly<{
    id: string;
    children?: readonly ElkNode[];
    edges?: readonly ElkExtendedEdge[];
  }>;

export type ELK = Readonly<{
  layout(graph: ElkNode): Promise<ElkNode>;
}>;
