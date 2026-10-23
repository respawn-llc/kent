import { act, renderHook, waitFor } from "@testing-library/react";
import { useAtomMount, useAtomSet, useAtomSuspense, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { expect, it, vi } from "bun:test";
import { useMemo, type ReactNode } from "react";
import { SidebarRootOwner, useOwnedSidebarRoots, useSidebarShell } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { appI18n } from "@/i18n";
import { SidebarProvider } from "./sidebarProvider";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { createAttentionViewModel } from "./AttentionViewModel";
import { attentionSubscriptionRoute, emitQuestionBatchRevision } from "@/test-support/api";
import type { DesktopSettings, StatusController } from "@/app-facade";
import { createBrowserNativeBridge } from "@/test-support/native-bridge";

async function deliveryFixture(initial: DesktopSettings["notifications"], focused: boolean) {
  const browser = createBrowserNativeBridge();
  const bridge = {
    ...browser,
    capabilities: {
      ...browser.capabilities,
      notifications: { ...browser.capabilities.notifications, basic: true },
    },
  };
  let saved = await bridge.settings.change({ field: "notifications", value: initial });
  const services = createTestServices([attentionSubscriptionRoute()], bridge);
  const notify = vi.spyOn(bridge.notifications, "notify").mockResolvedValue();
  vi.spyOn(bridge.notifications, "removeActive").mockResolvedValue();
  const status = { push: vi.fn<StatusController["push"]>(), dismiss: vi.fn<StatusController["dismiss"]>() };
  const openSessionChat = vi.fn(async () => undefined);
  const wrapper = ({ children }: { children: ReactNode }) => (
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
            openSessionChat,
            inputs: () => ({ focused, picker: null, delivery: saved.notifications }),
          }),
        [client, roots],
      );
      const model = useAtomValue(description);
      useAtomMount(model.surfaces);
      useAtomSuspense(model.deliver);
      useAtomSuspense(model.activate);
      useAtomSuspense(model.remove);
      useAtomSuspense(model.refresh);
      useAtomSuspense(model.inbox);
      return { surfaces: useAtomValue(model.surfaces), activate: useAtomSet(model.activate) };
    },
    { wrapper },
  );
  await act(async () => undefined);
  const emit = async (revision: number) => {
    await act(async () => {
      emitQuestionBatchRevision(services.transport, revision);
    });
  };
  return {
    view,
    emit,
    notify,
    status,
    openSessionChat,
    save: async (delivery: DesktopSettings["notifications"]) => {
      await act(async () => {
        saved = await bridge.settings.change({ field: "notifications", value: delivery });
      });
    },
  };
}

it("delivers Native attention while focused", async () => {
  const fixture = await deliveryFixture("native", true);
  await fixture.emit(1);
  await waitFor(() => {
    expect(fixture.notify).toHaveBeenCalledTimes(1);
  });
  expect(fixture.status.push).not.toHaveBeenCalled();
  fixture.view.unmount();
});

it("delivers In-app attention while unfocused", async () => {
  const fixture = await deliveryFixture("inApp", false);
  await fixture.emit(1);
  await waitFor(() => {
    expect(fixture.status.push).toHaveBeenCalledTimes(1);
  });
  expect(fixture.notify).not.toHaveBeenCalled();
  fixture.view.unmount();
});

it.each([true, false])("keeps Automatic focus-dependent delivery (focused=%s)", async (focused) => {
  const fixture = await deliveryFixture("automatic", focused);
  await fixture.emit(1);
  await waitFor(() => {
    expect(focused ? fixture.status.push : fixture.notify).toHaveBeenCalledTimes(1);
  });
  expect(focused ? fixture.notify : fixture.status.push).not.toHaveBeenCalled();
  fixture.view.unmount();
});

it("uses the newly saved delivery for a newer batch revision without replaying on save", async () => {
  const fixture = await deliveryFixture("inApp", true);
  await fixture.emit(1);
  await waitFor(() => {
    expect(fixture.status.push).toHaveBeenCalledTimes(1);
  });
  const notice = fixture.status.push.mock.calls[0]?.[0];
  await fixture.save("native");
  expect(fixture.notify).not.toHaveBeenCalled();
  expect(fixture.status.dismiss).not.toHaveBeenCalled();
  await fixture.emit(2);
  await waitFor(() => {
    expect(fixture.notify).toHaveBeenCalledTimes(1);
  });
  expect(fixture.status.dismiss).toHaveBeenCalledWith(notice?.id);
  expect(fixture.status.push).toHaveBeenCalledTimes(1);
  fixture.view.unmount();
});

it("applies Native delivery to a newer revision after activation failed", async () => {
  const fixture = await deliveryFixture("inApp", true);
  fixture.openSessionChat.mockRejectedValue(new Error("open failed"));
  await fixture.emit(1);
  await waitFor(() => {
    expect(fixture.status.push).toHaveBeenCalledTimes(1);
  });
  const record = [...fixture.view.result.current.surfaces.values()][0];
  if (record === undefined) throw new Error("Missing delivered attention");
  await act(async () => {
    fixture.view.result.current.activate({
      target: record.notification.target,
      notification: record.notification,
    });
  });
  await waitFor(() => {
    expect([...fixture.view.result.current.surfaces.values()][0]?.state).toBe("activation_failed");
  });
  await fixture.save("native");
  expect(fixture.notify).not.toHaveBeenCalled();
  await fixture.emit(2);
  await waitFor(() => {
    expect(fixture.notify).toHaveBeenCalledTimes(1);
  });
  expect(fixture.status.push).toHaveBeenCalledTimes(1);
  fixture.view.unmount();
});

it("keeps the replacement sidebar when the original notification activation finishes", async () => {
  const services = createTestServices([]);
  const focus = vi.spyOn(services.nativeBridge.window, "focusMain").mockResolvedValue();
  const status = { push: vi.fn(), dismiss: vi.fn() };
  const openSessionChat = vi.fn(async () => undefined);
  const inputs = () => ({ focused: true, picker: null, delivery: "automatic" as const });
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
