import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "bun:test";

import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { WorkflowParameterOutput } from "./WorkflowParameterOutput";

it("retains Markdown interaction and path copying without native inspection in a browser", async () => {
  const base = createTestServices([]);
  const load = vi.fn(base.nativeBridge.files.loadOutputFile);
  const write = vi.fn(async () => undefined);
  const services = {
    ...base,
    nativeBridge: {
      ...base.nativeBridge,
      capabilities: { ...base.nativeBridge.capabilities, clipboard: { writeText: true, readText: false } },
      files: { ...base.nativeBridge.files, loadOutputFile: load },
      clipboard: { ...base.nativeBridge.clipboard, writeText: write },
    },
  };
  render(
    <TestAppProviders services={services}>
      <WorkflowParameterOutput
        root={{ kind: "unavailable" }}
        value={"**Result** [web](https://example.com) ![photo](./a%2520%23%3F.png)"}
      />
    </TestAppProviders>,
  );
  expect(screen.getByRole("link").getAttribute("href")).toBe("https://example.com/");
  await userEvent.click(screen.getByRole("button"));
  await waitFor(() => {
    expect(write).toHaveBeenCalledWith("./a%20#?.png");
  });
  expect(load).not.toHaveBeenCalled();
});

it("isolates a linked local image action from its surrounding Markdown link", async () => {
  const base = createTestServices([]);
  const external = vi.fn(async () => undefined);
  const write = vi.fn(async () => undefined);
  const services = {
    ...base,
    nativeBridge: {
      ...base.nativeBridge,
      capabilities: {
        ...base.nativeBridge.capabilities,
        clipboard: { writeText: true, readText: false },
      },
      links: { ...base.nativeBridge.links, openExternal: external },
      clipboard: { ...base.nativeBridge.clipboard, writeText: write },
    },
  };
  render(
    <TestAppProviders services={services}>
      <WorkflowParameterOutput
        root={{ kind: "unavailable" }}
        value="[![artifact](./artifact.png)](https://example.com/review)"
      />
    </TestAppProviders>,
  );
  const navigation = vi.spyOn(window, "open").mockReturnValue(null);
  try {
    expect(fireEvent.click(screen.getByRole("button"))).toBe(false);
    await waitFor(() => {
      expect(write).toHaveBeenCalledWith("./artifact.png");
    });
    expect(external).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.getByRole("button")).not.toBeDisabled());
    expect(
      fireEvent(
        screen.getByRole("button"),
        new MouseEvent("auxclick", { button: 1, bubbles: true, cancelable: true }),
      ),
    ).toBe(false);
    await waitFor(() => {
      expect(write).toHaveBeenCalledTimes(2);
    });
    expect(external).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("link"));
    expect(external).toHaveBeenCalledWith("https://example.com/review");
  } finally {
    navigation.mockRestore();
  }
});

it("loads a visible absolute file without a Task root and keeps its open target after decoder failure", async () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "IntersectionObserver");
  const previousEventTarget = Object.getOwnPropertyDescriptor(globalThis, "EventTarget");
  // Happy DOM's exposed EventTarget is not the constructor in its Node prototype chain.
  const nodeParent: unknown = Object.getPrototypeOf(Node.prototype);
  if (nodeParent === null) throw new Error("DOM Node has no EventTarget prototype");
  const domEventTarget: unknown = Object.getOwnPropertyDescriptor(nodeParent, "constructor")?.value;
  if (domEventTarget === undefined) throw new Error("DOM EventTarget constructor is unavailable");
  Object.defineProperty(globalThis, "EventTarget", { configurable: true, value: domEventTarget });
  const observations = new Map<Element, () => void>();
  Object.defineProperty(globalThis, "IntersectionObserver", {
    configurable: true,
    value: class {
      constructor(
        private readonly notify: (entries: { target: Element; isIntersecting: boolean }[]) => void,
      ) {}
      observe(target: Element) {
        observations.set(target, () => {
          this.notify([{ target, isIntersecting: true }]);
        });
      }
      unobserve(target: Element) {
        observations.delete(target);
      }
      disconnect() {
        observations.clear();
      }
    },
  });
  const base = createTestServices([]);
  const target = { path: "/tmp/artifact.png" };
  const load = vi.fn(async () => ({
    kind: "image" as const,
    target,
    url: "blob:artifact",
    release: vi.fn(),
  }));
  const open = vi.fn(async () => undefined);
  const services = {
    ...base,
    nativeBridge: {
      ...base.nativeBridge,
      capabilities: {
        ...base.nativeBridge.capabilities,
        files: { ...base.nativeBridge.capabilities.files, stat: true, open: true },
      },
      files: { ...base.nativeBridge.files, loadOutputFile: load, openDefault: open },
    },
  };
  const view = render(
    <TestAppProviders services={services}>
      <WorkflowParameterOutput root={{ kind: "unavailable" }} value={target.path} />
    </TestAppProviders>,
  );
  try {
    await waitFor(() => {
      expect(observations.size).toBeGreaterThan(0);
    });
    act(() => {
      for (const show of observations.values()) show();
    });
    fireEvent.error(await screen.findByRole("img"));
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button"));
    await waitFor(() => {
      expect(open).toHaveBeenCalledWith(target);
    });
    expect(load).toHaveBeenCalledWith(target);
    expect(load).toHaveBeenCalledTimes(1);
  } finally {
    view.unmount();
    if (previous === undefined) Reflect.deleteProperty(globalThis, "IntersectionObserver");
    else Object.defineProperty(globalThis, "IntersectionObserver", previous);
    if (previousEventTarget === undefined) Reflect.deleteProperty(globalThis, "EventTarget");
    else Object.defineProperty(globalThis, "EventTarget", previousEventTarget);
  }
});
