import { act, render, screen, waitFor } from "@testing-library/react";
import { expect, it, mock, vi } from "bun:test";

import * as Stream from "effect/Stream";

let currentWindow: unknown;
const actualTauriWindow = await import("@tauri-apps/api/window");
await mock.module("@tauri-apps/api/window", () => ({
  ...actualTauriWindow,
  getCurrentWindow: () => currentWindow,
}));

const [
  { createTauriWindowFocusControls },
  { createTestServices, TestAppProviders },
  { createBrowserNativeBridge },
  { deferred },
  { useWindowFocus },
] = await Promise.all([
  import("@app/native-bridge"),
  import("@/test-support/app-services"),
  import("@/test-support/native-bridge"),
  import("@/test-support/chat-runtime"),
  import("./nativeHooks"),
]);

function Reader({ id }: Readonly<{ id: string }>) {
  const focused = useWindowFocus();
  return <output data-testid={id}>{JSON.stringify(focused)}</output>;
}

it("shares focus observation across readers and releases it after the owner leaves", async () => {
  const bridge = createBrowserNativeBridge();
  const observe = vi.spyOn(bridge.window, "focusChanges");
  const register = vi.spyOn(window, "addEventListener");
  const remove = vi.spyOn(window, "removeEventListener");
  const services = createTestServices([], bridge);
  const view = render(
    <TestAppProviders services={services}>
      <Reader id="first" />
      <Reader id="second" />
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(observe).toHaveBeenCalledTimes(1);
    expect(register.mock.calls.some(([event]) => event === "focus")).toBe(true);
  });
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  await waitFor(() => {
    expect(screen.getByTestId("first")).toHaveTextContent("true");
    expect(screen.getByTestId("second")).toHaveTextContent("true");
  });
  view.rerender(
    <TestAppProviders services={services}>
      <Reader id="second" />
    </TestAppProviders>,
  );
  await act(async () => {
    window.dispatchEvent(new Event("blur"));
  });
  await waitFor(() => {
    expect(screen.getByTestId("second")).toHaveTextContent("false");
  });
  expect(observe).toHaveBeenCalledTimes(1);
  view.unmount();
  await waitFor(() => {
    expect(remove.mock.calls.some(([event]) => event === "focus")).toBe(true);
  });
  remove.mockRestore();
  register.mockRestore();
});

it("releases a native focus registration that completes after its observation leaves", async () => {
  const registration = deferred<() => void>();
  const unlisten = vi.fn();
  const nativeWindow = {
    isFocused: vi.fn(async () => false),
    onFocusChanged: vi.fn(async () => registration.promise),
  };
  currentWindow = nativeWindow;
  const nativeFocus = createTauriWindowFocusControls();
  const browserBridge = createBrowserNativeBridge();
  const services = createTestServices([], {
    ...browserBridge,
    window: {
      ...browserBridge.window,
      isFocused: nativeFocus.isFocused,
      focusChanges: nativeFocus.focusChanges,
    },
  });
  const view = render(
    <TestAppProviders services={services}>
      <Reader id="focus" />
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(nativeWindow.onFocusChanged).toHaveBeenCalledTimes(1);
  });

  vi.useFakeTimers();
  try {
    await act(async () => {
      view.unmount();
      vi.runAllTimers();
    });
    await act(async () => {
      registration.resolve(unlisten);
    });
  } finally {
    vi.useRealTimers();
  }

  await waitFor(() => {
    expect(unlisten).toHaveBeenCalledTimes(1);
  });
});

it("releases a native focus registration that completes after its observation leaves", async () => {
  const registration = deferred<() => void>();
  const unlisten = vi.fn();
  const nativeWindow = {
    isFocused: vi.fn(async () => false),
    onFocusChanged: vi.fn(async () => registration.promise),
  };
  currentWindow = nativeWindow;
  const nativeFocus = createTauriWindowFocusControls();
  const browserBridge = createBrowserNativeBridge();
  const services = createTestServices([], {
    ...browserBridge,
    window: {
      ...browserBridge.window,
      isFocused: nativeFocus.isFocused,
      focusChanges: nativeFocus.focusChanges,
    },
  });
  const view = render(
    <TestAppProviders services={services}>
      <Reader id="focus" />
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(nativeWindow.onFocusChanged).toHaveBeenCalledTimes(1);
  });

  vi.useFakeTimers();
  try {
    await act(async () => {
      view.unmount();
      vi.runAllTimers();
    });
    await act(async () => {
      registration.resolve(unlisten);
    });
  } finally {
    vi.useRealTimers();
  }

  await waitFor(() => {
    expect(unlisten).toHaveBeenCalledTimes(1);
  });
});

it("keeps an observed focus event when the initial read completes later", async () => {
  const bridge = createBrowserNativeBridge();
  const initial = deferred<boolean>();
  const register = vi.spyOn(window, "addEventListener");
  vi.spyOn(bridge.window, "isFocused").mockReturnValue(initial.promise);
  render(
    <TestAppProviders services={createTestServices([], bridge)}>
      <Reader id="focus" />
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(register.mock.calls.some(([event]) => event === "focus")).toBe(true);
  });
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  await waitFor(() => {
    expect(screen.getByTestId("focus")).toHaveTextContent("true");
  });
  await act(async () => {
    initial.resolve(false);
  });
  expect(screen.getByTestId("focus")).toHaveTextContent("true");
  register.mockRestore();
});

it("logs a failed focus registration and remains unfocused after a late initial read", async () => {
  const bridge = createBrowserNativeBridge();
  const initial = deferred<boolean>();
  vi.spyOn(bridge.window, "isFocused").mockReturnValue(initial.promise);
  vi.spyOn(bridge.window, "focusChanges").mockReturnValue(Stream.fail(new Error("registration")));
  const services = createTestServices([], bridge);
  render(
    <TestAppProviders services={services}>
      <Reader id="focus" />
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(screen.getByTestId("focus")).toHaveTextContent("false");
  });
  await act(async () => {
    initial.resolve(true);
  });
  expect(screen.getByTestId("focus")).toHaveTextContent("false");
  expect(services.logger.entries()).toHaveLength(1);
});
