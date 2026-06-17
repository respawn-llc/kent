export type WorkflowProjectEvent = Readonly<{
  action: string;
  changedIDs: readonly string[];
  projectID: string;
  resource: string;
  workflowID: string;
}>;

export function workflowProjectEvent(params: unknown): WorkflowProjectEvent | null {
  if (!isRecord(params) || !("event" in params)) {
    return null;
  }
  const rawEvent = params.event;
  if (!isRecord(rawEvent)) {
    return null;
  }
  return {
    action: stringField(rawEvent, "action"),
    changedIDs: stringArrayField(rawEvent, "changed_ids"),
    projectID: stringField(rawEvent, "project_id"),
    resource: stringField(rawEvent, "resource"),
    workflowID: stringField(rawEvent, "workflow_id"),
  };
}

export function workflowProjectEventCanChangeAttention(params: unknown): boolean {
  const event = workflowProjectEvent(params);
  return event !== null && attentionResources.has(event.resource);
}

export function workflowProjectQuestionTaskID(params: unknown): string | null {
  const event = workflowProjectEvent(params);
  if (event?.resource !== "task" || !questionActions.has(event.action)) {
    return null;
  }
  const taskID = event.changedIDs[0] ?? "";
  return taskID.length > 0 ? taskID : null;
}

// workflowProjectEventAffectsTask reports whether a project event mutates the
// given task in a way that changes its detail representation. The server emits
// every task-affecting action (created/updated/started/interrupted/resumed/
// approved/moved/canceled/completed/comment_*/question_*) as a "task" resource
// event whose first changed id is the task id, so a structured resource +
// changed-id match reliably covers all of them without enumerating actions.
export function workflowProjectEventAffectsTask(params: unknown, taskID: string): boolean {
  const trimmedTaskID = taskID.trim();
  if (trimmedTaskID.length === 0) {
    return false;
  }
  const event = workflowProjectEvent(params);
  return event !== null && event.resource === "task" && event.changedIDs.includes(trimmedTaskID);
}

const attentionResources = new Set(["task", "workflow", "workflow_link"]);
const questionActions = new Set(["question_waiting", "question_cleared", "question_answered"]);

function stringField(value: Readonly<Record<string, unknown>>, key: string): string {
  const raw = value[key];
  return typeof raw === "string" ? raw : "";
}

function stringArrayField(value: Readonly<Record<string, unknown>>, key: string): readonly string[] {
  const raw = value[key];
  return Array.isArray(raw) ? raw.filter((item): item is string => typeof item === "string") : [];
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
