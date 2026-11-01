import { act, renderHook, waitFor } from "@testing-library/react";
import { useAtomMount, useAtomSet, useAtomSuspense, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { expect, it, vi } from "bun:test";
import { useMemo, type ReactNode } from "react";
import { SidebarRootOwner, useOwnedSidebarRoots, useSidebarShell, type StatusController } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { appI18n } from "@/i18n";
import { SidebarProvider } from "./sidebarProvider";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { createAttentionViewModel } from "./AttentionViewModel";
import * as Stream from "effect/Stream";
import { validateNativeNotification } from "@app/native-bridge";
import { turnCompletionPresentation } from "./turnCompletionPresentation";

it.each([
  { focused: true, title: "Session" },
  { focused: false, title: "Session" },
  { focused: true, title: "🙂".repeat(90) },
  { focused: false, title: "🙂".repeat(90) },
])("routes global turn-end feedback and opens its Session", async ({ focused, title }) => {
  const services = createTestServices([]);
  const target = { kind: "session_prompt", projectID: "project-1", sessionID: "session-1" } as const;
  vi.spyOn(services.api, "subscribeAttentionNotifications").mockReturnValue(
    Stream.make({
      kind: "event",
      event: {
        type: "session_turn_end",
        sequence: 1,
        shortID: null,
        sessionName: title,
        assistantContent: "Completed work",
        completedAt: 1,
        target,
      },
    }),
  );
  const bridge = services.nativeBridge;
  const configured = {
    ...services,
    nativeBridge: {
      ...bridge,
      capabilities: {
        ...bridge.capabilities,
        notifications: { ...bridge.capabilities.notifications, basic: true },
      },
    },
  };
  vi.spyOn(bridge.notifications, "permissionState").mockResolvedValue("granted");
  const notify = vi.spyOn(bridge.notifications, "notify").mockImplementation(async (notification) => {
    validateNativeNotification(notification);
  });
  vi.spyOn(bridge.window, "focusMain").mockResolvedValue();
  const status = { push: vi.fn<StatusController["push"]>(), dismiss: vi.fn() };
  const openSessionChat = vi.fn(async () => undefined);
  const inputs = () => ({ focused, picker: { projectID: target.projectID, sessionID: target.sessionID } });
  const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
    <TestAppProviders services={configured}>
      <SidebarProvider policy={sidebarDestinationPolicy}>
        <SidebarRootOwner>{children}</SidebarRootOwner>
      </SidebarProvider>
    </TestAppProviders>
  );
  const view = renderHook(
    () => {
      const roots = useOwnedSidebarRoots();
      const client = useQueryClient();
      const description = useMemo(
        () =>
          createAttentionViewModel({
            services: configured,
            client,
            roots,
            status,
            t: appI18n.t,
            inputs,
            openSessionChat,
          }),
        [client, roots],
      );
      const model = useAtomValue(description);
      useAtomMount(model.inbox);
      useAtomSuspense(model.activate);
      return useAtomSet(model.activate);
    },
    { wrapper },
  );
  await waitFor(() => {
    expect(focused ? status.push : notify).toHaveBeenCalledTimes(1);
  });
  expect(focused ? notify : status.push).not.toHaveBeenCalled();
  const presentation = focused ? status.push.mock.calls[0]?.[0] : notify.mock.calls[0]?.[0];
  expect(presentation).toMatchObject(
    turnCompletionPresentation({
      title: appI18n.t("app.attention.sessionEnded", { name: title }),
      body: "Completed work",
    }),
  );
  await act(async () => {
    if (focused) status.push.mock.calls[0]?.[0].onClick?.();
    else {
      const delivered = notify.mock.calls[0]?.[0];
      if (delivered === undefined) throw new Error("Missing native delivery");
      view.result.current({ target: delivered.target, notification: null });
    }
  });
  await waitFor(() => {
    expect(openSessionChat).toHaveBeenCalledWith({
      projectID: target.projectID,
      sessionID: target.sessionID,
    });
  });
  view.unmount();
});

it("keeps the replacement sidebar when the original notification activation finishes", async () => {
  const services = createTestServices([]);
  const focus = vi.spyOn(services.nativeBridge.window, "focusMain").mockResolvedValue();
  const status = { push: vi.fn(), dismiss: vi.fn() };
  const openSessionChat = vi.fn(async () => undefined);
  const inputs = () => ({ focused: true, picker: null });
  const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
    <TestAppProviders services={services}>
      <SidebarProvider policy={sidebarDestinationPolicy}>
        <SidebarRootOwner>{children}</SidebarRootOwner>
      </SidebarProvider>
    </TestAppProviders>
  );
  const view = renderHook(
    () => {
      const roots = useOwnedSidebarRoots();
      const client = useQueryClient();
      const description = useMemo(
        () =>
          createAttentionViewModel({
            services,
            client,
            roots,
            status,
            t: appI18n.t,
            inputs,
            openSessionChat,
          }),
        [client, roots],
      );
      const model = useAtomValue(description);
      useAtomMount(model.surfaces);
      useAtomSuspense(model.activate);
      return { activate: useAtomSet(model.activate), shell: useSidebarShell() };
    },
    { wrapper },
  );
  await act(async () => {
    view.result.current.activate({
      target: { kind: "task_detail", taskID: "task-a", focus: { kind: "interrupted_current_node" } },
      notification: null,
    });
  });
  await waitFor(() => {
    expect(view.result.current.shell.activeDestination).toMatchObject({ taskID: "task-a" });
  });
  await act(async () => {
    view.result.current.activate({
      target: { kind: "task_detail", taskID: "task-b", focus: { kind: "interrupted_current_node" } },
      notification: null,
    });
  });
  await waitFor(() => {
    expect(view.result.current.shell.activeDestination).toMatchObject({ taskID: "task-b" });
  });
  expect(focus).toHaveBeenCalledTimes(2);
  view.unmount();
});
