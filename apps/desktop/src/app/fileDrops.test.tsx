import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";

import { createTestServices, startupRoutes } from "@/test-support/app-services";
import { AppProviders } from "./AppProviders";

describe("window file drops", () => {
  it("inserts absolute paths at the focused input selection and updates React state", async () => {
    const services = createTestServices(startupRoutes);
    let drop: ((paths: readonly string[]) => void) | undefined;
    vi.spyOn(services.nativeBridge.window, "fileDrops").mockReturnValue(
      Stream.callback((queue) =>
        Effect.sync(() => {
          drop = (paths) => {
            Queue.offerUnsafe(queue, paths);
          };
        }),
      ),
    );
    function Editor() {
      const [value, setValue] = useState("before replace after");
      return (
        <>
          <textarea
            aria-label="draft"
            value={value}
            onChange={(event) => {
              setValue(event.target.value);
            }}
          />
          <output data-testid="draft">{value}</output>
        </>
      );
    }
    render(
      <AppProviders services={services}>
        <Editor />
      </AppProviders>,
    );
    await waitFor(() => {
      expect(drop).toBeDefined();
    });
    const input = screen.getByRole<HTMLTextAreaElement>("textbox");
    input.focus();
    input.setSelectionRange(7, 14);
    await act(async () => {
      drop?.(["/tmp/my image.png"]);
    });
    expect(input.value).toBe("before /tmp/my image.png after");
    expect(screen.getByTestId("draft")).toHaveTextContent("before /tmp/my image.png after");
    expect(input.selectionStart).toBe(24);
    expect(input.selectionEnd).toBe(24);
    fireEvent.change(input, { target: { value: `${input.value}!` } });
    expect(input.value).toBe("before /tmp/my image.png after!");
  });

  it("ignores drops without a focused editable input", async () => {
    const services = createTestServices(startupRoutes);
    let drop: ((paths: readonly string[]) => void) | undefined;
    vi.spyOn(services.nativeBridge.window, "fileDrops").mockReturnValue(
      Stream.callback((queue) =>
        Effect.sync(() => {
          drop = (paths) => {
            Queue.offerUnsafe(queue, paths);
          };
        }),
      ),
    );
    render(
      <AppProviders services={services}>
        <textarea defaultValue="unchanged" />
        <input aria-label="read only" readOnly defaultValue="locked" />
        <input aria-label="numeric" type="number" defaultValue="42" />
        <button>Other focus</button>
      </AppProviders>,
    );
    await waitFor(() => {
      expect(drop).toBeDefined();
    });
    const textarea = screen.getAllByRole<HTMLTextAreaElement>("textbox")[0];
    await act(async () => {
      drop?.(["/tmp/image.png"]);
    });
    screen.getByRole("button").focus();
    await act(async () => {
      drop?.(["/tmp/image.png"]);
    });
    screen.getByLabelText("read only").focus();
    await act(async () => {
      drop?.(["/tmp/image.png"]);
    });
    screen.getByLabelText("numeric").focus();
    await act(async () => {
      drop?.(["/tmp/image.png"]);
    });
    expect(textarea?.value).toBe("unchanged");
    expect(screen.getByLabelText("read only")).toHaveValue("locked");
    expect(screen.getByLabelText("numeric")).toHaveValue(42);
  });

  it("blocks browser file navigation without intercepting board drags and removes handlers on unmount", async () => {
    const services = createTestServices(startupRoutes);
    const unlisten = vi.fn<() => void>();
    vi.spyOn(services.nativeBridge.window, "fileDrops").mockReturnValue(
      Stream.callback(() => Effect.addFinalizer(() => Effect.sync(unlisten))),
    );
    const { unmount } = render(
      <AppProviders services={services}>
        <input />
      </AppProviders>,
    );
    await waitFor(() => {
      expect(services.nativeBridge.window.fileDrops).toHaveBeenCalled();
    });
    for (const type of ["dragover", "drop"]) {
      const fileEvent = new Event(type, { bubbles: true, cancelable: true });
      Object.defineProperty(fileEvent, "dataTransfer", { value: { types: ["Files"] } });
      expect(fireEvent(document, fileEvent)).toBe(false);
      const boardEvent = new Event(type, { bubbles: true, cancelable: true });
      Object.defineProperty(boardEvent, "dataTransfer", { value: { types: ["application/x-kent-task"] } });
      expect(fireEvent(document, boardEvent)).toBe(true);
    }
    unmount();
    await waitFor(() => {
      expect(unlisten).toHaveBeenCalledOnce();
    });
    const fileEvent = new Event("drop", { bubbles: true, cancelable: true });
    Object.defineProperty(fileEvent, "dataTransfer", { value: { types: ["Files"] } });
    expect(fireEvent(document, fileEvent)).toBe(true);
  });
});
