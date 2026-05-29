import { describe, expect, it } from "vitest";

import type { WorkflowGraphNode } from "./workflowGraphLayout";
import {
  graphNodeHeight,
  graphNodeWidth,
  workflowNodeHeight,
  workflowNodeWidth,
} from "./workflowGraphLayoutGeometry";

describe("workflowGraphLayoutGeometry", () => {
  it("falls back from invalid dimensions and accepts CSS pixel strings", () => {
    expect(graphNodeWidth(graphNode({ width: "220px", height: "92px" }))).toBe(220);
    expect(graphNodeHeight(graphNode({ width: "220px", height: "92px" }))).toBe(92);
    expect(graphNodeWidth(graphNode({ width: "bad", height: Number.NaN }))).toBe(workflowNodeWidth);
    expect(graphNodeHeight(graphNode({ width: "bad", height: Number.NaN }))).toBe(workflowNodeHeight);
  });
});

function graphNode(style: NonNullable<WorkflowGraphNode["style"]>): WorkflowGraphNode {
  return {
    data: {
      empty: false,
      entityID: "node-1",
      entityKind: "group",
      hasError: false,
      kind: "group",
      label: "Group",
    },
    id: "node-1",
    position: { x: 0, y: 0 },
    style,
  };
}
