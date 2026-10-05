import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { appI18n } from "@/i18n";
import {
  backlogTaskFixture,
  taskStartRoute,
  taskStartApplied,
  taskStartNeedsDependencies,
  mountTaskDetailSurface,
  taskDetailResponse,
} from "@/test-support/task-detail";

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
