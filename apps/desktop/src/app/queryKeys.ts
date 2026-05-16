export const queryKeys = {
  startup: ["startup"],
  readiness: ["startup", "readiness"],
  capabilities: ["startup", "capabilities"],
  projects: ["projects"],
  attention: (projectID: string) => ["attention", projectID],
  workspaces: (projectID: string) => ["workspaces", projectID],
  board: (projectID: string, workflowID: string) => ["board", projectID, workflowID],
  task: (taskID: string) => ["task", taskID],
  activity: (taskID: string) => ["activity", taskID],
  pendingAsks: (sessionID: string) => ["pending-asks", sessionID],
};
