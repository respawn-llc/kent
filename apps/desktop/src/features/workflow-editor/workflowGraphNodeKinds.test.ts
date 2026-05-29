import { describe, expect, it } from "vitest";

import {
  hasWorkflowNodeMetadataTooltip,
  isInspectableWorkflowNodeKind,
} from "./workflowGraphNodeKinds";

describe("workflowGraphNodeKinds", () => {
  it("keeps start, agent, join, and terminal nodes inspectable", () => {
    expect(["start", "agent", "join", "terminal"].filter(isInspectableWorkflowNodeKind)).toEqual([
      "start",
      "agent",
      "join",
      "terminal",
    ]);
    expect(isInspectableWorkflowNodeKind("unsupported")).toBe(false);
  });

  it("keeps metadata tooltip only on internal join nodes", () => {
    expect(hasWorkflowNodeMetadataTooltip("agent")).toBe(false);
    expect(hasWorkflowNodeMetadataTooltip("start")).toBe(false);
    expect(hasWorkflowNodeMetadataTooltip("join")).toBe(true);
    expect(hasWorkflowNodeMetadataTooltip("terminal")).toBe(false);
  });
});
