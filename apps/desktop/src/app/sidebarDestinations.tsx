/* eslint-disable react-refresh/only-export-components -- Sidebar destination rendering and titles share the typed destination registry. */
import type { ReactElement } from "react";
import type { useTranslation } from "react-i18next";

import { TaskDetailSurface } from "../features/task-detail/TaskDetailDialog";
import { NewTaskForm } from "../features/tasks/NewTaskDialog";
import { LinkWorkflowSidebar } from "../features/workflows/LinkWorkflowSidebar";
import { WorkflowCreateForm } from "../features/workflows/WorkflowCreateForm";
import { useAppNavigation } from "./navigation";
import type { SidebarController, SidebarDestination } from "./sidebarContext";

export function SidebarDestinationView({
  destination,
  resolveSidebar,
}: Readonly<{
  destination: SidebarDestination;
  resolveSidebar: SidebarController["resolveSidebar"];
}>): ReactElement {
  if (destination.kind === "newTask") {
    return (
      <NewTaskForm
        boardQueryWorkflowID={destination.boardQueryWorkflowID}
        className="w-full"
        onSubmitted={() => {
          resolveSidebar({ destination: "newTask", status: "submitted" });
        }}
        projectID={destination.projectID}
        workflowID={destination.workflowID}
      />
    );
  }

  if (destination.kind === "taskDetail") {
    return (
      <TaskDetailSurface
        enabled
        onMutated={destination.onMutated}
        resumeRunId={destination.resumeRunID}
        taskId={destination.taskID}
      />
    );
  }

  if (destination.kind === "workflowCreate") {
    return <WorkflowCreateDestinationView destination={destination} resolveSidebar={resolveSidebar} />;
  }

  if (destination.kind === "linkWorkflow") {
    return <LinkWorkflowDestinationView destination={destination} resolveSidebar={resolveSidebar} />;
  }

  return <>{destination.content}</>;
}

function LinkWorkflowDestinationView({
  destination,
  resolveSidebar,
}: Readonly<{
  destination: Extract<SidebarDestination, { kind: "linkWorkflow" }>;
  resolveSidebar: SidebarController["resolveSidebar"];
}>): ReactElement {
  const navigation = useAppNavigation();

  return (
    <LinkWorkflowSidebar
      onCreated={(workflowID) => {
        resolveSidebar({ destination: "workflow", status: "completed", workflowID });
        void navigation.openWorkflowEditor({
          projectID: destination.projectID,
          workflowID,
        });
      }}
      onLinked={(workflowID) => {
        resolveSidebar({ destination: "workflow", status: "completed", workflowID });
        void navigation.openProject(destination.projectID, workflowID);
      }}
      projectID={destination.projectID}
      selectedWorkflowID={destination.selectedWorkflowID ?? ""}
    />
  );
}

function WorkflowCreateDestinationView({
  destination,
  resolveSidebar,
}: Readonly<{
  destination: Extract<SidebarDestination, { kind: "workflowCreate" }>;
  resolveSidebar: SidebarController["resolveSidebar"];
}>): ReactElement {
  const navigation = useAppNavigation();

  return (
    <WorkflowCreateForm
      onCreated={(result) => {
        resolveSidebar({ destination: "workflow", status: "completed", workflowID: result.workflow.id });
        void navigation.openWorkflowEditor({
          projectID: destination.projectID,
          workflowID: result.workflow.id,
        });
      }}
      projectID={destination.projectID}
    />
  );
}

export function sidebarTitle(
  destination: SidebarDestination,
  t: ReturnType<typeof useTranslation>["t"],
): string {
  if (destination.kind === "newTask") {
    return t("task.newTitle");
  }
  if (destination.kind === "taskDetail") {
    return t("task.title");
  }
  if (destination.kind === "workflowCreate") {
    return t("workflowLibrary.createWorkflow");
  }
  if (destination.kind === "linkWorkflow") {
    return t("workflowLibrary.linkWorkflow");
  }
  return destination.title;
}
