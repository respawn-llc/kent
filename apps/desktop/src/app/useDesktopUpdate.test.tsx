import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createBrowserNativeBridge, type NativeBridge } from "@/test-support/native-bridge";
import { deferred } from "@/test-support/chat-runtime";
import { useDesktopUpdate } from "./useDesktopUpdate";

function availableBridge(): NativeBridge {
  const browser = createBrowserNativeBridge();
  return {
    ...browser,
    capabilities: { ...browser.capabilities, updater: true },
    updates: {
      ...browser.updates,
      supported: async () => true,
      check: async () => ({
        available: true,
        version: "2.0.0",
        currentVersion: "1.0.0",
        notes: null,
        publishedAt: null,
      }),
    },
  };
}

it("admits one installation for repeated submissions and waits for installation before relaunch", async () => {
  const bridge = availableBridge();
  const installed = deferred<undefined>();
  const download = vi
    .spyOn(bridge.updates, "downloadAndInstall")
    .mockReturnValue(Stream.fromEffect(Effect.promise(async () => installed.promise)).pipe(Stream.drain));
  const relaunch = vi.spyOn(bridge.updates, "relaunch").mockResolvedValue();
  const services = createTestServices([], bridge);
  const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
    <TestAppProviders services={services}>{children}</TestAppProviders>
  );
  const view = renderHook(() => useDesktopUpdate(bridge, services.logger), { wrapper });
  await waitFor(() => {
    expect(view.result.current.phase).toBe("available");
  });
  await act(async () => {
    view.result.current.install();
    view.result.current.install();
  });
  expect(download).toHaveBeenCalledTimes(1);
  expect(relaunch).not.toHaveBeenCalled();
  expect(view.result.current.phase).toBe("installing");
  await act(async () => {
    installed.resolve(undefined);
  });
  await waitFor(() => {
    expect(relaunch).toHaveBeenCalledTimes(1);
  });
});

it("keeps an install failure actionable and does not relaunch", async () => {
  const bridge = availableBridge();
  const download = vi
    .spyOn(bridge.updates, "downloadAndInstall")
    .mockReturnValue(Stream.fail(new Error("download")));
  const relaunch = vi.spyOn(bridge.updates, "relaunch");
  const services = createTestServices([], bridge);
  const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
    <TestAppProviders services={services}>{children}</TestAppProviders>
  );
  const view = renderHook(() => useDesktopUpdate(bridge, services.logger), { wrapper });
  await waitFor(() => {
    expect(view.result.current.phase).toBe("available");
  });
  await act(async () => {
    view.result.current.install();
  });
  await waitFor(() => {
    expect(view.result.current.phase).toBe("error");
  });
  expect(relaunch).not.toHaveBeenCalled();
  expect(services.logger.entries().some((entry) => entry.level === "error")).toBe(true);
  await act(async () => {
    view.result.current.install();
  });
  expect(download).toHaveBeenCalledTimes(2);
});

it("keeps relaunch failure actionable after installation", async () => {
  const bridge = availableBridge();
  vi.spyOn(bridge.updates, "downloadAndInstall").mockReturnValue(Stream.empty);
  const relaunch = vi.spyOn(bridge.updates, "relaunch").mockRejectedValue(new Error("relaunch"));
  const services = createTestServices([], bridge);
  const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
    <TestAppProviders services={services}>{children}</TestAppProviders>
  );
  const view = renderHook(() => useDesktopUpdate(bridge, services.logger), { wrapper });
  await waitFor(() => {
    expect(view.result.current.phase).toBe("available");
  });
  await act(async () => {
    view.result.current.install();
  });
  await waitFor(() => {
    expect(view.result.current.phase).toBe("error");
  });
  expect(relaunch).toHaveBeenCalledTimes(1);
  await act(async () => {
    view.result.current.install();
  });
  expect(relaunch).toHaveBeenCalledTimes(2);
});

it("logs check failure without offering an update", async () => {
  const bridge = availableBridge();
  const check = vi.spyOn(bridge.updates, "check").mockRejectedValue(new Error("check"));
  const services = createTestServices([], bridge);
  const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
    <TestAppProviders services={services}>{children}</TestAppProviders>
  );
  const view = renderHook(() => useDesktopUpdate(bridge, services.logger), { wrapper });
  await waitFor(() => {
    expect(check).toHaveBeenCalledTimes(1);
    expect(services.logger.entries().some((entry) => entry.context.error === "check")).toBe(true);
  });
  expect(view.result.current.phase).toBe("none");
});
