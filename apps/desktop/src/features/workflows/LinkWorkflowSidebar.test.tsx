import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, mock, vi } from "bun:test";
import type { ProjectWorkflowLink, WorkflowRecord } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { appI18n } from "@/i18n";
import { WorkflowActionsContextMenuStub, WorkflowListStub } from "@/test-support/workflow-rendering";

const actualWorkflowLibrary = await import("@/shared/workflow-library");
const actualUi = await import("@/ui");
await mock.module("@/shared/workflow-library", () => ({
  ...actualWorkflowLibrary,
  WorkflowActionsContextMenu: WorkflowActionsContextMenuStub,
}));
await mock.module("@/ui", () => ({
  ...actualUi,
  VirtualizedInfiniteList: WorkflowListStub,
}));
const { LinkWorkflowSidebar } = await import("./LinkWorkflowSidebar");

it("lets another Workflow link while the first mounted row is pending", async () => {
  const services = createTestServices([]);
  const workflows = ["a", "b"].map((id): WorkflowRecord => ({
    id,
    name: id,
    description: "",
    version: 1,
    executionTargetPolicy: { mode: "default_branch", customRef: null },
  }));
  vi.spyOn(services.api, "listWorkflows").mockResolvedValue({ workflows, nextOffset: null });
  vi.spyOn(services.api, "listProjectWorkflowLinks").mockResolvedValue([]);
  const a = deferred<ProjectWorkflowLink>();
  const b = deferred<ProjectWorkflowLink>();
  const link = vi
    .spyOn(services.api, "linkWorkflowToProject")
    .mockImplementation(async ({ workflowID }) => (workflowID === "a" ? a.promise : b.promise));
  const onLinked = vi.fn();
  const view = render(
    <TestAppProviders services={services}>
      <LinkWorkflowSidebar creating={false} onCreated={vi.fn()} onLinked={onLinked} projectID="project-1" />
    </TestAppProviders>,
  );
  const buttons = await screen.findAllByRole("button", { name: appI18n.t("workflowLibrary.link") });
  const [first, second] = buttons;
  if (first === undefined || second === undefined) throw new Error("Expected two Workflow rows");
  act(() => {
    first.click();
    first.click();
  });
  await waitFor(() => {
    expect(link).toHaveBeenCalledTimes(1);
  });
  await waitFor(() => {
    expect(first).toBeDisabled();
  });
  expect(second).toBeEnabled();
  fireEvent.click(second);
  await waitFor(() => {
    expect(link).toHaveBeenCalledTimes(2);
  });
  view.unmount();
  await act(async () => {
    a.resolve({ id: "link-a", projectID: "project-1", workflowID: "a", isDefault: true });
    b.resolve({ id: "link-b", projectID: "project-1", workflowID: "b", isDefault: false });
  });
  await waitFor(() => {
    expect(onLinked).toHaveBeenCalledTimes(2);
  });
});
