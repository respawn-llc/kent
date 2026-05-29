import { describe, expect, it } from "vitest";

import { uniqueWorkflowModelKey, workflowModelKeyFromLabel } from "./workflowEditorGraphKeys";

describe("workflowEditorGraphKeys", () => {
  it("normalizes labels into workflow model keys", () => {
    expect(workflowModelKeyFromLabel("Code Review!", "agent")).toBe("code_review");
    expect(workflowModelKeyFromLabel("123 Done", "agent")).toBe("x_123_done");
    expect(workflowModelKeyFromLabel(" ", "Agent Node")).toBe("agent_node");
  });

  it("creates unique suffixed keys", () => {
    expect(uniqueWorkflowModelKey("Review", ["review", "review_2"])).toBe("review_3");
  });
});
