import { act, renderHook, waitFor } from "@testing-library/react";
import { useAtomMount, useAtomSet, useAtomSuspense, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, type ReactNode } from "react";
import { SidebarRootOwner, useOwnedSidebarRoots, useSidebarShell } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { appI18n } from "@/i18n";
import { SidebarProvider } from "./sidebarProvider";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { createAttentionViewModel } from "./AttentionViewModel";

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
