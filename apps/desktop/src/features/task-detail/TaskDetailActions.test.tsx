import { expect, it, vi } from "bun:test";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

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
import { createBrowserNativeBridge } from "@/test-support/native-bridge";

function nativeDesktopBridge(openWindow = vi.fn(async () => "created" as const)) {
  const bridge = createBrowserNativeBridge();
  return {
    ...bridge,
    capabilities: { ...bridge.capabilities, dialogWindows: true },
    dialogs: { ...bridge.dialogs, openWindow },
  };
}

it("orders Start, Open, and Task-wide Interrupt in one wrapping action flow", async () => {
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
  const interruptLabel = appI18n.t("board.interrupt");
  const open = within(flow).getByRole("button", { name: openLabel });
  const interrupt = within(flow).getByRole("button", { name: interruptLabel });
  expect(within(flow).getAllByRole("button")).toEqual([start, open, interrupt]);
  expect(open).toHaveAttribute("title", openLabel);
  expect(interrupt).toHaveAttribute("title", interruptLabel);
});

it("opens the only Session directly without a picker", async () => {
  const openSessionChat = vi.fn(async () => undefined);
  const sessionID = "33333333-3333-4333-8333-333333333333";
  mountTaskDetailSurface(
    taskActionFixture(taskDetailResponse, {}, [
      {
        sessionID,
        sessionName: "Review",
        nodeDisplayName: "Review",
      },
    ]),
    { openSessionChat },
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: appI18n.t("task.openInChat") }));
  expect(openSessionChat).toHaveBeenCalledTimes(1);
  expect(openSessionChat).toHaveBeenCalledWith({ projectID: "project-1", sessionID });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("Shift-clicks the only Session into its native Chat window", async () => {
  const openSessionChat = vi.fn(async () => undefined);
  const sessionID = "33333333-3333-4333-8333-333333333333";
  const openWindow = vi.fn(async () => "created" as const);
  mountTaskDetailSurface(
    taskActionFixture(taskDetailResponse, {}, [
      {
        sessionID,
        sessionName: "Review",
        nodeDisplayName: "Review",
      },
    ]),
    {
      nativeBridge: nativeDesktopBridge(openWindow),
      openSessionChat,
    },
  );

  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("task.openInChat") }), {
    shiftKey: true,
  });

  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledWith(
      expect.objectContaining({
        params: { projectID: "project-1", sessionID },
      }),
    );
  });
  expect(openSessionChat).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("Shift-clicks Open Chat then opens the ordinary picker selection natively", async () => {
  const openings: string[] = [];
  const openWindow = vi.fn(async () => "created" as const);
  mountTaskDetailSurface(taskDetailResponse, {
    nativeBridge: nativeDesktopBridge(openWindow),
    openSessionChat: async (target) => {
      openings.push(target.sessionID);
    },
  });

  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("task.openInChat") }), {
    shiftKey: true,
  });
  const picker = within(screen.getByRole("dialog"));
  const user = userEvent.setup();
  await user.click(picker.getByRole("button", { name: "Review chat" }));
  await user.click(screen.getByRole("button", { name: appI18n.t("task.openInChat") }));
  const ordinaryPicker = within(screen.getByRole("dialog"));
  await user.click(ordinaryPicker.getByRole("button", { name: "44444444-4444-4444-8444-444444444444" }));

  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  expect(openWindow).toHaveBeenCalledWith(
    expect.objectContaining({
      params: {
        projectID: "project-1",
        sessionID: "33333333-3333-4333-8333-333333333333",
      },
    }),
  );
  expect(openings).toEqual(["44444444-4444-4444-8444-444444444444"]);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("Shift-clicks a Session in the ordinary Open Chat picker natively", async () => {
  const openings: string[] = [];
  const openWindow = vi.fn(async () => "created" as const);
  mountTaskDetailSurface(taskDetailResponse, {
    nativeBridge: nativeDesktopBridge(openWindow),
    openSessionChat: async (target) => {
      openings.push(target.sessionID);
    },
  });
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: appI18n.t("task.openInChat") }));
  const picker = within(screen.getByRole("dialog"));
  fireEvent.click(picker.getByRole("button", { name: "Review chat" }), { shiftKey: true });

  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledWith(
      expect.objectContaining({
        params: {
          projectID: "project-1",
          sessionID: "33333333-3333-4333-8333-333333333333",
        },
      }),
    );
  });
  expect(openings).toEqual([]);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("offers a CLI Session picker and keeps Task-wide Interrupt generic", async () => {
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
  const open = within(flow).getByRole("button", { name: appI18n.t("task.openInCli") });
  const interrupt = within(flow).getByRole("button", { name: appI18n.t("board.interrupt") });
  expect(within(flow).getAllByRole("button")).toEqual([open, interrupt]);
  await userEvent.setup().click(open);
  const popup = within(screen.getByRole("dialog"));
  expect(popup.getByRole("button", { name: "33333333-3333-4333-8333-333333333333" })).toBeInTheDocument();
  expect(popup.getByRole("button", { name: "Review" })).toBeInTheDocument();
});

it("keeps Interrupt generic when a Script is the live target", async () => {
  mountTaskDetailSurface(taskActionFixture(taskDetailResponseWithCurrentScript, { canInterrupt: true }));

  const flow = await screen.findByTestId("task-detail-action-flow");
  const interrupt = within(flow).getByRole("button", { name: appI18n.t("board.interrupt") });
  expect(within(flow).getAllByRole("button").at(-1)).toBe(interrupt);
});

it("selects one Session from the Open in Chat popup without opening the others", async () => {
  const targets: { projectID: string; sessionID: string }[] = [];
  mountTaskDetailSurface(taskDetailResponse, {
    openSessionChat: async (target) => {
      targets.push(target);
    },
  });

  const flow = await screen.findByTestId("task-detail-action-flow");
  const openChat = within(flow).getByRole("button", { name: appI18n.t("task.openInChat") });
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
  await user.click(openChat);
  expect(targets).toEqual([]);
  const popup = within(screen.getByRole("dialog"));
  expect(popup.getByRole("button", { name: "44444444-4444-4444-8444-444444444444" })).toBeInTheDocument();
  await user.click(popup.getByRole("button", { name: "Review chat" }));

  expect(targets).toEqual([{ projectID: "project-1", sessionID: "33333333-3333-4333-8333-333333333333" }]);
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
  expect(startTask).toHaveBeenCalledTimes(1);
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
