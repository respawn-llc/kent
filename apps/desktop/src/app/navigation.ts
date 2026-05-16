import { useNavigate } from "@tanstack/react-router";

export type AppNavigation = Readonly<{
  openHome(): void;
  openSettings(): void;
  openProject(projectID: string, workflowID?: string): void;
  openTask(taskID: string): void;
  openProjectTask(projectID: string, workflowID: string, taskID: string): void;
  closeProjectTask(projectID: string, workflowID: string): void;
}>;

export function useAppNavigation(): AppNavigation {
  const navigate = useNavigate();
  return {
    openHome() {
      void navigate({ to: "/" });
    },
    openSettings() {
      void navigate({ to: "/settings" });
    },
    openProject(projectID, workflowID = "") {
      void navigate({
        to: "/projects/$projectId",
        params: { projectId: projectID },
        search: { workflowId: workflowID, taskId: "", resumeRunId: "" },
      });
    },
    openTask(taskID) {
      void navigate({ to: "/tasks/$taskId", params: { taskId: taskID } });
    },
    openProjectTask(projectID, workflowID, taskID) {
      void navigate({
        to: "/projects/$projectId",
        params: { projectId: projectID },
        search: { workflowId: workflowID, taskId: taskID, resumeRunId: "" },
      });
    },
    closeProjectTask(projectID, workflowID) {
      void navigate({
        to: "/projects/$projectId",
        params: { projectId: projectID },
        search: { workflowId: workflowID, taskId: "", resumeRunId: "" },
      });
    },
  };
}
