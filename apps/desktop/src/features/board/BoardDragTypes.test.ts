import { describe, expect, it } from "vitest";

import { decodeBoardCardDragPayload, encodeBoardCardDragPayload } from "./BoardDragTypes";

describe("board card drag payloads", () => {
  it("round-trips structured drag permissions without encoding ids in MIME types", () => {
    const payload = {
      taskID: "task-1",
      canStart: true,
      activeNodeIDs: ["backlog"],
      statusKind: "backlog",
      manualMoveTargetNodeIDs: ["node-review", "node-done"],
    };

    expect(decodeBoardCardDragPayload(encodeBoardCardDragPayload(payload))).toEqual(payload);
  });

  it("rejects malformed drag payloads", () => {
    expect(decodeBoardCardDragPayload("")).toBeNull();
    expect(decodeBoardCardDragPayload("not-json")).toBeNull();
    expect(
      decodeBoardCardDragPayload('{"taskID":"task-1","canStart":true,"manualMoveTargetNodeIDs":[5]}'),
    ).toBeNull();
    expect(
      decodeBoardCardDragPayload('{"taskID":"","canStart":true,"manualMoveTargetNodeIDs":[]}'),
    ).toBeNull();
    expect(
      decodeBoardCardDragPayload(
        '{"taskID":"task-1","canStart":true,"activeNodeIDs":[],"statusKind":"","manualMoveTargetNodeIDs":[]}',
      ),
    ).toBeNull();
  });
});
