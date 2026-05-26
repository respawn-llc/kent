import type { TaskStatus } from "../../api";
import { taskStatusTone } from "./taskStatusTone";

describe("taskStatusTone", () => {
  it("maps fixed and remaining task statuses to semantic badge tones", () => {
    expect(taskStatusTone(taskStatus({ kind: "canceled", nativeState: "canceled" }))).toBe("danger");
    expect(taskStatusTone(taskStatus({ kind: "done", nativeState: "terminal" }))).toBe("success");
    expect(taskStatusTone(taskStatus({ kind: "running", nativeState: "running" }))).toBe("info");
    expect(taskStatusTone(taskStatus({ kind: "waiting_approval", nativeState: "waiting_approval" }))).toBe(
      "warning",
    );
    expect(taskStatusTone(taskStatus({ attentionTypes: ["question"], kind: "blocked", nativeState: "idle" }))).toBe(
      "warning",
    );
    expect(taskStatusTone(taskStatus({ kind: "interrupted", nativeState: "interrupted" }))).toBe("danger");
    expect(taskStatusTone(taskStatus({ kind: "backlog", nativeState: "backlog" }))).toBe("neutral");
  });
});

function taskStatus(overrides: Partial<TaskStatus>): TaskStatus {
  return {
    attentionTypes: [],
    kind: "backlog",
    label: "Backlog",
    nativeState: "backlog",
    nodeIDs: [],
    runIDs: [],
    ...overrides,
  };
}
