import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { TaskDetailSessionChatEntry } from "@/features/task-detail";
import { appI18n } from "@/i18n";
import {
  mountTaskDetailSurface,
  taskDetailResponse,
  taskDetailResponseWithCurrentScript,
  taskActionFixture,
  taskStartRoute,
  taskStartApplied,
} from "@/test-support/task-detail";
import { projectEventsFixture } from "@/test-support/project-events";

it("orders Start, Open, and targeted Interrupt in one wrapping action flow", async () => {
  const sessionName = `${"👨‍👩‍👧‍👦".repeat(31)}e\u0301x`;
  mountTaskDetailSurface(
    taskActionFixture(taskDetailResponse, { canStart: true }, [
      {
        sessionID: "33333333-3333-4333-8333-333333333333",
        sessionName,
        nodeDisplayName: "Code Review",
      },
    ]),
  );

  const flow = await screen.findByTestId("task-detail-action-flow");
  const start = within(flow).getByTestId("task-detail-start");
  const openLabel = appI18n.t("task.openInCli", { name: sessionName });
  const interruptLabel = appI18n.t("task.interruptChat", { name: sessionName });
  const open = within(flow).getByRole("button", { name: openLabel });
  const interrupt = within(flow).getByRole("button", { name: interruptLabel });
  expect(within(flow).getAllByRole("button")).toEqual([start, open, interrupt]);
  expect(open).toHaveAttribute("title", openLabel);
  expect(interrupt).toHaveAttribute("title", interruptLabel);
});

it("falls back to the Agent Node display name and keeps Task-wide Interrupt generic", async () => {
  mountTaskDetailSurface(
    taskActionFixture(taskDetailResponse, {}, [
      {
        sessionID: "33333333-3333-4333-8333-333333333333",
        nodeDisplayName: "Implementation",
      },
      {
        sessionID: "44444444-4444-4444-8444-444444444444",
        sessionName: "Review",
        nodeDisplayName: "Code Review",
      },
    ]),
  );

  const flow = await screen.findByTestId("task-detail-action-flow");
  const firstOpen = within(flow).getByRole("button", {
    name: appI18n.t("task.openInCli", { name: "Implementation" }),
  });
  const secondOpen = within(flow).getByRole("button", {
    name: appI18n.t("task.openInCli", { name: "Review" }),
  });
  const interrupt = within(flow).getByRole("button", { name: appI18n.t("board.interrupt") });
  expect(within(flow).getAllByRole("button")).toEqual([firstOpen, secondOpen, interrupt]);
});

it("keeps Interrupt generic when a Script is the live target", async () => {
  mountTaskDetailSurface(taskActionFixture(taskDetailResponseWithCurrentScript, { canInterrupt: true }));

  const flow = await screen.findByTestId("task-detail-action-flow");
  const interrupt = within(flow).getByRole("button", { name: appI18n.t("board.interrupt") });
  expect(within(flow).getAllByRole("button").at(-1)).toBe(interrupt);
});

it("replaces Open in CLI with Open Chat for every live Session", async () => {
  const targets: Parameters<TaskDetailSessionChatEntry>[0][] = [];
  mountTaskDetailSurface(taskDetailResponse, {
    openSessionChat: async (target) => {
      targets.push(target);
    },
  });

  const flow = await screen.findByTestId("task-detail-action-flow");
  const openChatButtons = [
    within(flow).getByRole("button", {
      name: appI18n.t("task.openChat", { name: "Review chat" }),
    }),
    within(flow).getByRole("button", {
      name: appI18n.t("task.openChat", { name: "Implementation" }),
    }),
  ];
  expect(
    within(flow).queryByRole("button", {
      name: appI18n.t("task.openInCli", { name: "Review chat" }),
    }),
  ).not.toBeInTheDocument();
  expect(
    within(flow).queryByRole("button", {
      name: appI18n.t("task.openInCli", { name: "Implementation" }),
    }),
  ).not.toBeInTheDocument();
  const user = userEvent.setup();
  for (const openChat of openChatButtons) {
    await user.click(openChat);
  }

  expect(targets).toEqual([
    { projectID: "project-1", sessionID: "33333333-3333-4333-8333-333333333333" },
    { projectID: "project-1", sessionID: "44444444-4444-4444-8444-444444444444" },
  ]);
});

it("shows Start loading, prevents duplicate requests, and permits it after an independent observation fails", async () => {
  let resolveStart: ((value: ReturnType<typeof taskStartApplied>) => void) | undefined;
  const services = mountTaskDetailSurface(
    taskActionFixture(taskDetailResponse, { canStart: true, canInterrupt: false }, []),
    {
      routes: [
        taskStartRoute(
          async () =>
            new Promise<ReturnType<typeof taskStartApplied>>((resolve) => {
              resolveStart = resolve;
            }),
        ),
      ],
    },
  );
  const user = userEvent.setup();
  const startTask = vi.spyOn(services.api, "startTask");
  const start = await screen.findByTestId("task-detail-start");

  await user.click(start);
  await waitFor(() => {
    expect(start).toHaveAttribute("aria-busy", "true");
  });
  expect(start).toBeEnabled();
  await user.click(start);
  expect(startTask).toHaveBeenCalledOnce();
  resolveStart?.(taskStartApplied());
  await waitFor(() => {
    expect(start).toHaveAttribute("aria-busy", "false");
  });

  act(() => {
    projectEventsFixture(services.transport).fail(new Error("offline"));
  });
  const error = await screen.findByTestId("error-state");
  expect(screen.queryByTestId("task-detail-start")).not.toBeInTheDocument();
  await user.click(within(error).getByRole("button", { name: appI18n.t("app.retry") }));
  expect(await screen.findByTestId("task-detail-start")).toBeEnabled();
});
