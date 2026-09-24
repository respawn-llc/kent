import type { WorkflowProjectEvent } from "@/api";

export function shouldRefreshWorkflowEditor(
  event: WorkflowProjectEvent,
  projectID: string | null,
  workflowID: string,
): boolean {
  return (
    shouldRefreshWorkflowDefinition(event, workflowID) ||
    shouldRefreshWorkflowLink(event, projectID, workflowID)
  );
}

export function shouldNotifyWorkflowEditorRefresh(
  event: WorkflowProjectEvent,
  projectID: string | null,
  workflowID: string,
): boolean {
  if (
    event.resource === "workflow" &&
    event.workflowID === workflowID &&
    workflowDefinitionActions.has(event.action)
  )
    return event.action !== "deleted";
  if (
    event.resource === "workflow_link" &&
    event.projectID === projectID &&
    workflowLinkActions.has(event.action) &&
    event.workflowID === workflowID
  )
    return event.action !== "unlinked";
  return false;
}

export function shouldRefreshWorkflowDefinition(event: WorkflowProjectEvent, workflowID: string): boolean {
  return (
    event.resource === "workflow" &&
    event.workflowID === workflowID &&
    workflowDefinitionActions.has(event.action)
  );
}

export function shouldRefreshWorkflowLink(
  event: WorkflowProjectEvent,
  projectID: string | null,
  workflowID: string,
): boolean {
  return (
    event.resource === "workflow_link" &&
    event.projectID === projectID &&
    workflowLinkActions.has(event.action) &&
    event.workflowID === workflowID
  );
}

const workflowDefinitionActions = new Set(["updated", "deleted", "graph_saved"]);
const workflowLinkActions = new Set(["linked", "default_changed", "unlinked"]);
