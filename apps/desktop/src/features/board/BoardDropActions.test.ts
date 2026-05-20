import { describe, expect, it } from "vitest";

import type { BoardColumn } from "../../api";
import type { BoardCardDragPayload } from "./BoardDragTypes";
import { classifyDrop } from "./BoardDropActions";

describe("classifyDrop", () => {
  it("does not force target-union output fields onto allowed manual moves", () => {
    expect(
      classifyDrop(
        {
          ...baseColumn,
          id: "node-review",
          transitionOutputFields: [{ name: "summary", description: "Summary" }],
        },
        { ...baseDragPayload, manualMoveTargetNodeIDs: ["node-review"] },
        undefined,
      ),
    ).toEqual({ kind: "move" });
  });
});

const baseDragPayload: BoardCardDragPayload = {
  activeNodeIDs: ["node-current"],
  canStart: false,
  manualMoveTargetNodeIDs: [],
  statusKind: "active",
  taskID: "task-1",
};

const baseColumn: BoardColumn = {
  assigneeRole: "",
  groupID: "",
  id: "node-target",
  isBacklog: false,
  isDone: false,
  key: "target",
  kind: "agent",
  name: "Target",
  outputFields: [],
  sortOrder: 0,
  taskCount: 0,
  transitionOutputFields: [],
};
