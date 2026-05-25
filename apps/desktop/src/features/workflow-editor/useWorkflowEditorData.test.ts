import { describe, expect, it } from "vitest";

import {
  shouldRefreshWorkflowDefinition,
  shouldRefreshWorkflowEditor,
  shouldRefreshWorkflowLink,
} from "./useWorkflowEditorData";

describe("shouldRefreshWorkflowEditor", () => {
  it("refreshes workflow definition events for the open workflow only", () => {
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({ action: "node_updated", resource: "workflow", workflow_id: "workflow-1" }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(true);
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({ action: "updated", resource: "workflow", workflow_id: "workflow-1" }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(true);
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({ action: "graph_saved", resource: "workflow", workflow_id: "workflow-1" }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(true);
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({ action: "deleted", resource: "workflow", workflow_id: "workflow-1" }),
        "",
        "workflow-1",
      ),
    ).toBe(true);
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({ action: "node_updated", resource: "workflow", workflow_id: "workflow-2" }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(false);
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({ action: "created", resource: "task", workflow_id: "workflow-1" }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(false);
  });

  it("refreshes active project workflow-link events that may affect the open workflow", () => {
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({
          action: "unlinked",
          changed_ids: ["link-1"],
          project_id: "project-1",
          resource: "workflow_link",
          workflow_id: "workflow-1",
        }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(true);
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({
          action: "unlinked",
          changed_ids: ["link-2"],
          project_id: "project-1",
          resource: "workflow_link",
          workflow_id: "workflow-2",
        }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(false);
    expect(
      shouldRefreshWorkflowEditor(
        eventParams({
          action: "unlinked",
          project_id: "project-2",
          resource: "workflow_link",
          workflow_id: "workflow-1",
        }),
        "project-1",
        "workflow-1",
      ),
    ).toBe(false);
  });

  it("separates workflow definition events from project workflow-link events", () => {
    const workflowEvent = eventParams({
      action: "graph_saved",
      resource: "workflow",
      workflow_id: "workflow-1",
    });
    const linkEvent = eventParams({
      action: "default_changed",
      changed_ids: ["link-1"],
      project_id: "project-1",
      resource: "workflow_link",
      workflow_id: "workflow-1",
    });

    expect(shouldRefreshWorkflowDefinition(workflowEvent, "workflow-1")).toBe(true);
    expect(shouldRefreshWorkflowDefinition(linkEvent, "workflow-1")).toBe(false);
    expect(shouldRefreshWorkflowLink(workflowEvent, "project-1", "workflow-1")).toBe(false);
    expect(shouldRefreshWorkflowLink(linkEvent, "project-1", "workflow-1")).toBe(true);
  });
});

function eventParams(event: Readonly<Record<string, unknown>>) {
  return { event };
}
