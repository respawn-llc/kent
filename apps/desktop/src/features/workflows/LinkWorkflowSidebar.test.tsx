import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import type { ProjectWorkflowLink, WorkflowRecord } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { appI18n } from "@/i18n";
import type * as UiModule from "@/ui";
import type * as WorkflowLibrary from "@/shared/workflow-library";
import { LinkWorkflowSidebar } from "./LinkWorkflowSidebar";

vi.mock("@/shared/workflow-library", async (original) => ({
  ...(await original<typeof WorkflowLibrary>()),
  WorkflowActionsContextMenu: ({ children }: { children: (loading: boolean) => ReactElement }) =>
    children(false),
}));

vi.mock("@/ui", async (original) => ({
  ...(await original<typeof UiModule>()),
  VirtualizedInfiniteList: ({
    items,
    renderItem,
  }: {
    items: readonly WorkflowRecord[];
    renderItem: (item: WorkflowRecord) => ReactNode;
  }) => (
    <>
      {items.map((item) => (
        <div key={item.id}>{renderItem(item)}</div>
      ))}
    </>
  ),
}));

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
  await waitFor(() => expect(first).toBeDisabled());
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
