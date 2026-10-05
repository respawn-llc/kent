import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { appI18n } from "@/i18n";
import type { WorkflowRecord } from "@/api";
import { WorkflowCreateForm } from "./WorkflowCreateForm";

it("admits one creation and retains its original completion after the form leaves", async () => {
  const services = createTestServices([]);
  const response = deferred<WorkflowRecord>();
  const create = vi.spyOn(services.api, "createWorkflow").mockReturnValue(response.promise);
  const onCreated = vi.fn();
  const replacement = vi.fn();
  const view = render(
    <TestAppProviders services={services}>
      <WorkflowCreateForm onCreated={onCreated} />
    </TestAppProviders>,
  );
  const name = screen.getByRole("textbox", { name: appI18n.t("workflowLibrary.name") });
  fireEvent.change(name, { target: { value: "Workflow" } });
  act(() => {
    name.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    name.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  await waitFor(() => {
    expect(create).toHaveBeenCalledTimes(1);
  });
  view.rerender(
    <TestAppProviders services={services}>
      <WorkflowCreateForm onCreated={replacement} />
    </TestAppProviders>,
  );
  view.unmount();
  await act(async () => {
    response.resolve({
      id: "workflow-1",
      name: "Workflow",
      description: "",
      version: 1,
      executionTargetPolicy: { mode: "default_branch", customRef: null },
    });
  });
  await waitFor(() => {
    expect(onCreated).toHaveBeenCalledOnce();
  });
  expect(replacement).not.toHaveBeenCalled();
});
