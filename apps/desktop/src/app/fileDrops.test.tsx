import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState, type ReactNode } from "react";
import { describe, expect, it, vi } from "bun:test";
import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";

import { createTestServices, startupRoutes } from "@/test-support/app-services";
import { AppProviders } from "./AppProviders";

describe("window file drops", () => {
  it.each([true, false])(
    "inserts absolute paths at the input selection and updates React state (focused=%s)",
    async (focused) => {
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
      installBrowserSetRangeText(input);
      showField(input);
      if (focused) input.focus();
      input.setSelectionRange(7, 14);
      await act(async () => {
        drop?.(["/tmp/my image.png"]);
      });
      await waitFor(() => {
        expect(input.value).toBe("before /tmp/my image.png after");
        expect(screen.getByTestId("draft")).toHaveTextContent("before /tmp/my image.png after");
      });
      expect(input.selectionStart).toBe(24);
      expect(input.selectionEnd).toBe(24);
      fireEvent.change(input, { target: { value: `${input.value}!` } });
      expect(input.value).toBe("before /tmp/my image.png after!");
    },
  );

  it("ignores drops with several editable inputs and no focused editable input", async () => {
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
        <textarea defaultValue="another draft" />
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

  it("inserts files into the sole visible editable field without focus", async () => {
    const drop = await mountDropFields(
      <>
        <textarea aria-label="draft" defaultValue="draft " />
        <textarea aria-label="hidden" hidden />
        <textarea aria-label="disabled" disabled />
        <input aria-label="readonly" readOnly />
        <input aria-label="numeric" type="number" />
        <button>Other focus</button>
      </>,
    );
    const draft = screen.getByLabelText<HTMLTextAreaElement>("draft");
    showField(draft);
    showField(screen.getByLabelText("hidden"));
    showField(screen.getByLabelText("disabled"));
    showField(screen.getByLabelText("readonly"));
    showField(screen.getByLabelText("numeric"));
    draft.setSelectionRange(6, 6);
    screen.getByRole("button").focus();
    await act(async () => {
      drop(["/tmp/one.png", "/tmp/two.txt"]);
    });
    expect(draft.value).toBe("draft /tmp/one.png /tmp/two.txt");
  });

  it.each(["hidden", "inert", "offscreen"] as const)(
    "ignores a %s input when finding the sole visible field",
    async (excluded) => {
      const drop = await mountDropFields(
        <>
          <textarea aria-label="draft" />
          <div data-testid="excluded-container">
            <textarea aria-label="excluded" />
          </div>
        </>,
      );
      const draft = screen.getByLabelText<HTMLTextAreaElement>("draft");
      const other = screen.getByLabelText<HTMLTextAreaElement>("excluded");
      showField(draft);
      showField(other);
      const parent = screen.getByTestId("excluded-container");
      if (excluded === "offscreen") {
        vi.spyOn(other, "getBoundingClientRect").mockReturnValue(new DOMRect(0, -100, 100, 30));
      } else parent.setAttribute(excluded, "");
      await act(async () => {
        drop(["/tmp/file.txt"]);
      });
      expect(draft.value).toBe("/tmp/file.txt");
      expect(other.value).toBe("");
    },
  );

  it.each(["text/uri-list", "text/plain"])(
    "inserts a dropped URL from %s into the sole visible field without focus",
    async (format) => {
      await mountDropFields(<textarea aria-label="draft" />);
      const draft = screen.getByLabelText<HTMLTextAreaElement>("draft");
      showField(draft);
      const transfer = new DataTransfer();
      transfer.setData(format, "https://example.com/page");
      fireEvent.drop(document, { dataTransfer: transfer });
      expect(draft.value).toBe("https://example.com/page");
    },
  );

  it("does not choose between several visible fields for a URL or file drop", async () => {
    const drop = await mountDropFields(
      <>
        <textarea aria-label="first" />
        <textarea aria-label="second" />
      </>,
    );
    const fields = screen.getAllByRole<HTMLTextAreaElement>("textbox");
    for (const field of fields) showField(field);
    await act(async () => {
      drop(["/tmp/file.txt"]);
    });
    const transfer = new DataTransfer();
    transfer.setData("text/uri-list", "https://example.com");
    fireEvent.drop(document, { dataTransfer: transfer });
    for (const field of fields) expect(field.value).toBe("");
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
      expect(unlisten).toHaveBeenCalledTimes(1);
    });
    const fileEvent = new Event("drop", { bubbles: true, cancelable: true });
    Object.defineProperty(fileEvent, "dataTransfer", { value: { types: ["Files"] } });
    expect(fireEvent(document, fileEvent)).toBe(true);
  });
});

function showField(field: HTMLElement): void {
  field.style.visibility = "visible";
  vi.spyOn(field, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 100, 30));
}

async function mountDropFields(children: ReactNode) {
  const services = createTestServices(startupRoutes);
  let receive: ((paths: readonly string[]) => void) | undefined;
  vi.spyOn(services.nativeBridge.window, "fileDrops").mockReturnValue(
    Stream.callback((queue) =>
      Effect.sync(() => {
        receive = (paths) => {
          Queue.offerUnsafe(queue, paths);
        };
      }),
    ),
  );
  render(<AppProviders services={services}>{children}</AppProviders>);
  await waitFor(() => {
    expect(receive).toBeDefined();
  });
  return (paths: readonly string[]) => {
    if (receive === undefined) throw new Error("Missing drop observation");
    receive(paths);
  };
}

function installBrowserSetRangeText(input: HTMLTextAreaElement): void {
  const trackedValue = Object.getOwnPropertyDescriptor(input, "value");
  if (trackedValue === undefined) throw new Error("Expected React to track the controlled textarea value.");
  const setRangeText = input.setRangeText.bind(input);
  Object.defineProperty(input, "setRangeText", {
    configurable: true,
    value: (...args: Parameters<HTMLTextAreaElement["setRangeText"]>) => {
      const [replacement, start, , selectionMode] = args;
      const selectionStart = start;
      // Browsers update the native value internally; Happy DOM routes through React's tracked setter.
      if (!Reflect.deleteProperty(input, "value"))
        throw new Error("Could not restore the native textarea value setter.");
      try {
        setRangeText(...args);
        if (selectionMode === "end") {
          // Happy DOM computes "end" from the whole value, not the inserted text.
          const caret = selectionStart + replacement.length;
          input.setSelectionRange(caret, caret);
        }
      } finally {
        Object.defineProperty(input, "value", trackedValue);
      }
    },
  });
}
