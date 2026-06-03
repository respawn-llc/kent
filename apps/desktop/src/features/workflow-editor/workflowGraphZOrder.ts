// React Flow's nodes pane must stay z-index:auto so individual node layers can
// straddle the edge pane: group backgrounds below edges, node cards/handles above edges.
export const workflowGraphZOrder = {
  group: 1,
  edge: 2,
  edgeLabel: 2,
  node: 3,
} as const;

export const workflowGraphLayerClassNames = {
  group: "workflow-graph-layer-group",
  node: "workflow-graph-layer-node",
  edge: "workflow-graph-layer-edge",
} as const;
