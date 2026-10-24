import type { WorkflowProjectEvent } from "@/api";

export function workflowProjectQuestionTaskID(event: WorkflowProjectEvent): string | null {
  if (event.resource !== "task" || !questionActions.has(event.action)) {
    return null;
  }
  return event.primaryEntityID;
}

export function workflowProjectEventCanChangeTaskSearch(event: WorkflowProjectEvent): boolean {
  return event.resource === "task" || (event.resource === "workflow" && event.action === "deleted");
}

const questionActions = new Set(["question_waiting", "question_cleared"]);
