import { render } from "@testing-library/react";
import type { DndContextProps, DragEndEvent } from "@dnd-kit/core";
import { afterEach, expect, it, vi } from "vitest";
import { DragDropSurface } from "./DragDropSurface";

const context = vi.hoisted(() => ({ props: null as DndContextProps | null }));

vi.mock("@dnd-kit/core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@dnd-kit/core")>()),
  DndContext: (props: DndContextProps) => {
    context.props = props;
    return null;
  },
}));

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  context.props = null;
});

it.each(["development", "production"])("cancels an unmeasured drop in %s", (mode) => {
  vi.stubEnv("DEV", mode === "development");
  const onCancel = vi.fn();
  const onDrop = vi.fn();
  const diagnostic = vi.spyOn(console, "error").mockImplementation(() => undefined);
  render(
    <DragDropSurface onCancel={onCancel} onDrop={onDrop}>
      {null}
    </DragDropSurface>,
  );
  const event: DragEndEvent = {
    activatorEvent: new Event("pointerdown"),
    active: { id: "card", data: { current: {} }, rect: { current: { initial: null, translated: null } } },
    collisions: null,
    delta: { x: 0, y: 0 },
    over: null,
  };
  const onDragEnd = context.props?.onDragEnd;
  if (!onDragEnd) throw new Error("Drag context did not mount.");
  if (mode === "development") expect(() => onDragEnd(event)).toThrow(Error);
  else {
    expect(() => onDragEnd(event)).not.toThrow();
    expect(diagnostic).toHaveBeenCalledWith(expect.any(Error));
  }
  expect(onCancel).toHaveBeenCalledOnce();
  expect(onDrop).not.toHaveBeenCalled();
});
