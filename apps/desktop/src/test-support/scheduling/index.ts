import { act } from "@testing-library/react";

export function installAnimationFrameTestSupport(): () => void {
  const originalGlobals = [
    ["requestAnimationFrame", Object.getOwnPropertyDescriptor(globalThis, "requestAnimationFrame")],
    ["cancelAnimationFrame", Object.getOwnPropertyDescriptor(globalThis, "cancelAnimationFrame")],
  ] as const;
  Object.defineProperty(globalThis, "requestAnimationFrame", {
    configurable: true,
    value: (callback: FrameRequestCallback) =>
      window.setTimeout(() => {
        callback(performance.now());
      }, 0),
    writable: true,
  });
  Object.defineProperty(globalThis, "cancelAnimationFrame", {
    configurable: true,
    value: (handle: number) => {
      window.clearTimeout(handle);
    },
    writable: true,
  });
  return () => {
    for (const [name, descriptor] of originalGlobals) {
      if (descriptor === undefined) Reflect.deleteProperty(globalThis, name);
      else Object.defineProperty(globalThis, name, descriptor);
    }
  };
}

export async function waitForMacrotask(): Promise<void> {
  await new Promise<void>((resolve) => {
    window.setTimeout(resolve, 0);
  });
}

export async function flushQueuedWork(): Promise<void> {
  await act(async () => {
    await waitForMacrotask();
  });
}
