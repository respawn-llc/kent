import { RegistryProvider, useAtomMount } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { createHomeCreationObservation } from "./HomeCreationObservation";

it("acquires the native listener only while the observation is mounted and releases it", async () => {
  vi.useFakeTimers();
  try {
    const services = createTestServices([]);
    vi.spyOn(services.nativeBridge.capabilities, "projectCreationWindow", "get").mockReturnValue(true);
    const unlisten = vi.fn();
    const register = vi.spyOn(services.nativeBridge.projectCreation, "onCreated").mockResolvedValue(unlisten);
    const model = createHomeCreationObservation({
      services,
      client: new QueryClient(),
      t: appI18n.t,
      push: vi.fn(),
      openProject: vi.fn(async () => undefined),
    });
    expect(register).not.toHaveBeenCalled();
    const view = renderHook(
      () => {
        useAtomMount(model);
      },
      {
        wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
      },
    );
    await act(async () => undefined);
    expect(register).toHaveBeenCalledOnce();
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    expect(unlisten).toHaveBeenCalledOnce();
  } finally {
    vi.useRealTimers();
  }
});

it("releases a listener whose registration finishes after disposal", async () => {
  vi.useFakeTimers();
  try {
    const services = createTestServices([]);
    vi.spyOn(services.nativeBridge.capabilities, "projectCreationWindow", "get").mockReturnValue(true);
    const registration = deferred<() => void>();
    const unlisten = vi.fn();
    vi.spyOn(services.nativeBridge.projectCreation, "onCreated").mockReturnValue(registration.promise);
    const model = createHomeCreationObservation({
      services,
      client: new QueryClient(),
      t: appI18n.t,
      push: vi.fn(),
      openProject: vi.fn(async () => undefined),
    });
    const view = renderHook(
      () => {
        useAtomMount(model);
      },
      {
        wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
      },
    );
    await act(async () => undefined);
    view.unmount();
    await act(async () => vi.advanceTimersByTimeAsync(500));
    await act(async () => {
      registration.resolve(unlisten);
    });
    expect(unlisten).toHaveBeenCalledOnce();
  } finally {
    vi.useRealTimers();
  }
});

it("surfaces registration failure without restarting observation", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.nativeBridge.capabilities, "projectCreationWindow", "get").mockReturnValue(true);
  const register = vi
    .spyOn(services.nativeBridge.projectCreation, "onCreated")
    .mockRejectedValue(new Error("Unavailable"));
  const push = vi.fn();
  const model = createHomeCreationObservation({
    services,
    client: new QueryClient(),
    t: appI18n.t,
    push,
    openProject: vi.fn(async () => undefined),
  });
  renderHook(
    () => {
      useAtomMount(model);
    },
    {
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  await act(async () => undefined);
  expect(push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
  expect(register).toHaveBeenCalledOnce();
});

it("refreshes Projects and opens the Project from a native creation event", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.nativeBridge.capabilities, "projectCreationWindow", "get").mockReturnValue(true);
  const register = vi.spyOn(services.nativeBridge.projectCreation, "onCreated").mockResolvedValue(vi.fn());
  const client = new QueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const openProject = vi.fn(async () => undefined);
  const model = createHomeCreationObservation({ services, client, t: appI18n.t, push: vi.fn(), openProject });
  renderHook(
    () => {
      useAtomMount(model);
    },
    {
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  await act(async () => {
    register.mock.calls[0]?.[0]({ projectID: "project-1" });
  });
  expect(invalidate).toHaveBeenCalled();
  expect(openProject).toHaveBeenCalledWith("project-1");
});

it("reports an overflowing burst and closes observation without restarting", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.nativeBridge.capabilities, "projectCreationWindow", "get").mockReturnValue(true);
  const unlisten = vi.fn();
  const register = vi.spyOn(services.nativeBridge.projectCreation, "onCreated").mockResolvedValue(unlisten);
  const push = vi.fn();
  const openProject = vi.fn(async () => undefined);
  const model = createHomeCreationObservation({
    services,
    client: new QueryClient(),
    t: appI18n.t,
    push,
    openProject,
  });
  renderHook(
    () => {
      useAtomMount(model);
    },
    {
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  await act(async () => {
    for (let index = 0; index < 2000; index += 1) {
      register.mock.calls[0]?.[0]({ projectID: `project-${index.toString()}` });
    }
  });
  await waitFor(() => {
    expect(push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
    expect(unlisten).toHaveBeenCalledOnce();
  });
  expect(register).toHaveBeenCalledOnce();
  expect(openProject.mock.calls.length).toBeLessThan(2000);
});
