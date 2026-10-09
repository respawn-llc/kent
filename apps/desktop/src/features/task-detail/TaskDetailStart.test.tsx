import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { appI18n } from "@/i18n";
import { RpcError } from "@/api";
import { deferred } from "@/test-support/chat-runtime";
import {
  backlogTaskFixture,
  taskStartRoute,
  taskStartApplied,
  taskStartNeedsDependencies,
  mountTaskDetailSurface,
  taskDetailResponse,
} from "@/test-support/task-detail";

it("replaces Start with a visible loading spinner and restores the action after failure", async () => {
  const services = mountTaskDetailSurface(backlogTaskFixture(taskDetailResponse), {
    routes: [taskStartRoute(taskStartApplied)],
  });
  const request = deferred<Awaited<ReturnType<typeof services.api.startTask>>>();
  const log = vi.spyOn(services.logger, "append");
  vi.spyOn(services.api, "startTask").mockReturnValueOnce(request.promise);
  const start = await screen.findByRole("button", { name: appI18n.t("task.start") });
  await userEvent.click(start);
  await waitFor(() => {
    expect(start).toHaveAttribute("aria-busy", "true");
  });
  expect(within(start).getByText(appI18n.t("task.start"))).not.toBeVisible();
  expect(within(start).getByTestId("spinner")).toBeVisible();
  const failure = {
    code: "internal_failure",
    detail: { case: "internalFailure", value: { cause: "preparation failed" } },
  };
  const error = new RpcError({
    code: -32603,
    method: "task.start",
    message: "start rejected",
    data: failure,
  });
  await act(async () => {
    request.reject(error);
  });
  await waitFor(() => {
    expect(start).toHaveAttribute("aria-busy", "false");
  });
  expect(within(start).getByTestId("spinner")).not.toBeVisible();
  expect(within(start).getByText(appI18n.t("task.start"))).toBeVisible();
  expect(start).toBeEnabled();
  const diagnostic = log.mock.calls.find(([, , context]) => context?.action === "start");
  expect(diagnostic?.[2]).toMatchObject({
    action: "start",
    method: error.method,
    code: String(error.code),
    details: JSON.stringify(failure),
  });
});

it("starts the persisted Task without submitting a dirty title or description draft", async () => {
  const services = mountTaskDetailSurface(backlogTaskFixture(taskDetailResponse), {
    routes: [taskStartRoute(taskStartApplied)],
  });
  const start = vi.spyOn(services.api, "startTask");
  const update = vi.spyOn(services.api, "updateTask");
  const user = userEvent.setup();

  const title = await screen.findByRole("textbox", { name: appI18n.t("task.name") });
  await user.clear(title);
  await user.type(title, "Unsaved title");
  await user.click(screen.getByRole("textbox", { name: appI18n.t("task.description") }));
  const description = await screen.findByRole("textbox", { name: appI18n.t("task.description") });
  await user.clear(description);
  await user.type(description, "Unsaved description");

  await user.click(screen.getByTestId("task-detail-start"));

  await waitFor(() => {
    expect(start).toHaveBeenCalledOnce();
  });
  expect(update).not.toHaveBeenCalled();
  expect(start.mock.calls[0]?.[0]).toMatchObject({
    taskID: "task-1",
    proceedDespiteDependencies: false,
  });
});

it("focuses Dependencies in-place for every View deps request without a sidebar host", async () => {
  const scrollTo = vi.fn();
  Object.defineProperty(HTMLElement.prototype, "scrollTo", {
    configurable: true,
    value: scrollTo,
  });
  mountTaskDetailSurface(backlogTaskFixture(taskDetailResponse), {
    routes: [taskStartRoute(() => taskStartNeedsDependencies(1))],
  });
  const user = userEvent.setup();
  const start = await screen.findByTestId("task-detail-start");

  await user.click(start);
  const firstViewDependencies = await screen.findByTestId("dependency-confirmation-view");
  const firstRequestBaseline = scrollTo.mock.calls.length;
  await user.click(firstViewDependencies);
  await waitFor(() => {
    expect(scrollTo.mock.calls.length).toBeGreaterThan(firstRequestBaseline);
  });

  await user.click(start);
  const secondViewDependencies = await screen.findByTestId("dependency-confirmation-view");
  const secondRequestBaseline = scrollTo.mock.calls.length;
  await user.click(secondViewDependencies);
  await waitFor(() => {
    expect(scrollTo.mock.calls.length).toBeGreaterThan(secondRequestBaseline);
  });
});
