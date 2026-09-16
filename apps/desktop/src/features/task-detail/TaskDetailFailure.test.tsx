import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { appI18n } from "@/i18n";
import {
  emptyTaskAttentionResponse,
  mountTaskDetailSurface,
  taskDetailResponse,
  taskUpdatedEvent,
} from "@/test-support/task-detail";

it("replaces Task content with page-level recovery when its live observation fails", async () => {
  const services = mountTaskDetailSurface(taskDetailResponse, { attention: emptyTaskAttentionResponse });
  await screen.findByTestId("task-detail-action-flow");
  act(() => {
    services.transport.fail("workflow.subscribeProject", new Error("observation unavailable"));
  });
  const error = await screen.findByTestId("error-state");
  expect(screen.queryByTestId("task-detail-action-flow")).not.toBeInTheDocument();
  await userEvent.click(within(error).getByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByTestId("task-detail-action-flow")).toBeInTheDocument();
  expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
});

it("replaces the page on attention failure and preserves its unsaved draft through Retry", async () => {
  let failed = false;
  const services = mountTaskDetailSurface(taskDetailResponse, {
    routes: [
      {
        method: "workflow.task.attention.list",
        handler: () => {
          if (failed) throw new Error("attention unavailable");
          return emptyTaskAttentionResponse;
        },
      },
    ],
  });
  const user = userEvent.setup();
  const title = await screen.findByRole("textbox", { name: appI18n.t("task.name") });
  await user.clear(title);
  await user.type(title, "Unsaved title");
  failed = true;
  act(() => {
    services.transport.emit("workflow.project", taskUpdatedEvent);
  });
  const error = await screen.findByTestId("error-state");
  expect(screen.queryByTestId("task-detail-action-flow")).not.toBeInTheDocument();
  failed = false;
  await user.click(within(error).getByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByRole("textbox", { name: appI18n.t("task.name") })).toHaveValue("Unsaved title");
  expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
});
