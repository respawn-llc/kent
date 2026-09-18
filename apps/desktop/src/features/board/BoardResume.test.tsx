import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as Stream from "effect/Stream";
import type { BoardCard, SelectedWorkflowBoard, TaskResumeResponse } from "@/api";
import { ChatPromptPresenceProvider } from "@/app-facade";
import { appI18n } from "@/i18n";
import { TestAppProviders, createTestServices, startupRoutes } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { TestSidebar } from "@/test-support/sidebar";
import { BoardRoute } from "./BoardRoute";

const workflow = {
  id: "workflow-1",
  name: "Workflow",
  description: "",
  version: 1,
  isProjectDefault: true,
  validForTaskCreation: true,
  validationErrors: [],
};
const board: SelectedWorkflowBoard = {
  projectID: "project-1",
  projectKey: "KNT",
  projectName: "Project",
  defaultWorkspaceID: "workspace-1",
  attachedWorkspaceCount: 1,
  selectedWorkflow: workflow,
  workflows: [workflow],
  groups: [],
  generatedAt: 1,
  columns: [
    {
      id: "node-1",
      key: "node",
      kind: "agent",
      name: "Doing",
      assigneeRole: "",
      outputFields: [],
      groupID: null,
      sortOrder: 0,
      isBacklog: false,
      isDone: false,
      taskCount: 2,
    },
  ],
};
const cards: readonly BoardCard[] = ["task-a", "task-b"].map((id) => ({
  id,
  shortID: id,
  title: id,
  preview: { markdown: "", truncated: false },
  workflowID: workflow.id,
  activeNodeIDs: ["node-1"],
  sourceWorkspace: {
    id: "workspace-1",
    name: "Workspace",
    rootPath: "/workspace",
    availability: "available",
    isPrimary: true,
    updatedAt: 1,
  },
  status: { kind: "interrupted", nativeState: "interrupted", nodeIDs: ["node-1"], attentionTypes: [] },
  actions: { canResume: true, canStart: false, canInterrupt: false, canDelete: true },
  labelIDs: [],
  dependencyProgress: null,
  updatedAt: 1,
}));

it("resumes another Board card while the first Resume remains pending", async () => {
  const services = createTestServices(startupRoutes);
  vi.spyOn(services.api, "getBoard").mockResolvedValue(board);
  vi.spyOn(services.api, "listBoardNodeCards").mockResolvedValue({
    projectID: board.projectID,
    workflowID: workflow.id,
    nodeID: "node-1",
    cards,
    nextOffset: null,
    generatedAt: 1,
  });
  vi.spyOn(services.api, "subscribeProject").mockReturnValue(Stream.never);
  const a = deferred<TaskResumeResponse>();
  const b = deferred<TaskResumeResponse>();
  const resume = vi
    .spyOn(services.api, "resumeTask")
    .mockImplementation(async ({ taskID }) => (taskID === "task-a" ? a.promise : b.promise));
  render(
    <TestAppProviders services={services}>
      <ChatPromptPresenceProvider>
        <TestSidebar>
          <BoardRoute projectId={board.projectID} workflowId={workflow.id} selectedTaskId="" />
        </TestSidebar>
      </ChatPromptPresenceProvider>
    </TestAppProviders>,
  );
  const buttons = await screen.findAllByRole("button", { name: appI18n.t("board.resume") });
  expect(buttons).toHaveLength(2);
  const [first, second] = buttons;
  if (!first || !second) throw new Error("Expected two Resume controls");
  fireEvent.click(first);
  await waitFor(() => {
    expect(resume).toHaveBeenCalledTimes(1);
  });
  expect(first).toHaveAttribute("aria-busy", "true");
  expect(second).toBeEnabled();
  fireEvent.click(second);
  await waitFor(() => {
    expect(resume.mock.calls.map(([input]) => input.taskID)).toEqual(["task-a", "task-b"]);
  });
  const applied: TaskResumeResponse = { outcome: "applied", applied: { currentNodes: [] } };
  await act(async () => {
    b.resolve(applied);
  });
  await waitFor(() => expect(second).not.toHaveAttribute("aria-busy", "true"));
  expect(first).toHaveAttribute("aria-busy", "true");
  await act(async () => {
    a.resolve(applied);
  });
  await waitFor(() => expect(first).not.toHaveAttribute("aria-busy", "true"));
});
