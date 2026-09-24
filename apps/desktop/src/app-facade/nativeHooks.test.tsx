import { act, render, screen, waitFor } from "@testing-library/react";

import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createBrowserNativeBridge } from "@/test-support/native-bridge";
import { deferred } from "@/test-support/chat-runtime";
import { useWindowFocus } from "./nativeHooks";
import * as Stream from "effect/Stream";

function Reader({ id }: Readonly<{ id: string }>) {
  const focused = useWindowFocus();
  return <output data-testid={id}>{JSON.stringify(focused)}</output>;
}

it("shares focus observation across readers and releases it after the owner leaves", async () => {
  const bridge = createBrowserNativeBridge();
  const observe = vi.spyOn(bridge.window, "focusChanges");
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
  });
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  expect(screen.getByTestId("first")).toHaveTextContent("true");
  expect(screen.getByTestId("second")).toHaveTextContent("true");
  view.rerender(
    <TestAppProviders services={services}>
      <Reader id="second" />
    </TestAppProviders>,
  );
  await act(async () => {
    window.dispatchEvent(new Event("blur"));
  });
  expect(screen.getByTestId("second")).toHaveTextContent("false");
  expect(observe).toHaveBeenCalledTimes(1);
  view.unmount();
  await waitFor(() => {
    expect(remove.mock.calls.some(([event]) => event === "focus")).toBe(true);
  });
  remove.mockRestore();
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
  await waitFor(() => expect(screen.getByTestId("focus")).toHaveTextContent("true"));
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
  await waitFor(() => expect(screen.getByTestId("focus")).toHaveTextContent("false"));
  await act(async () => {
    initial.resolve(true);
  });
  expect(screen.getByTestId("focus")).toHaveTextContent("false");
  expect(services.logger.entries()).toHaveLength(1);
});
