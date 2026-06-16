import { describe, expect, it } from "vitest";

import {
  workflowProjectEvent,
  workflowProjectEventCanChangeAttention,
  workflowProjectQuestionTaskID,
} from "./workflowProjectEvents";

describe("workflowProjectEvent", () => {
  it("parses workflow project event params", () => {
    expect(
      workflowProjectEvent({
        event: {
          action: "question_waiting",
          changed_ids: ["task-1", "run-1", "ask-1"],
          project_id: "project-1",
          resource: "task",
          workflow_id: "workflow-1",
        },
      }),
    ).toEqual({
      action: "question_waiting",
      changedIDs: ["task-1", "run-1", "ask-1"],
      projectID: "project-1",
      resource: "task",
      workflowID: "workflow-1",
    });
  });

  it("recognizes resources that can affect attention", () => {
    expect(workflowProjectEventCanChangeAttention({ event: { resource: "task" } })).toBe(true);
    expect(workflowProjectEventCanChangeAttention({ event: { resource: "workflow_link" } })).toBe(true);
    expect(workflowProjectEventCanChangeAttention({ event: { resource: "runtime_log" } })).toBe(false);
    expect(workflowProjectEventCanChangeAttention({})).toBe(false);
  });

  it("extracts task ids from question lifecycle events", () => {
    expect(
      workflowProjectQuestionTaskID({
        event: {
          action: "question_waiting",
          changed_ids: ["task-1", "run-1", "ask-1"],
          resource: "task",
        },
      }),
    ).toBe("task-1");
    expect(workflowProjectQuestionTaskID({ event: { action: "completed", changed_ids: ["task-1"], resource: "task" } })).toBeNull();
    expect(workflowProjectQuestionTaskID({ event: { action: "question_waiting", changed_ids: [], resource: "task" } })).toBeNull();
  });
});
