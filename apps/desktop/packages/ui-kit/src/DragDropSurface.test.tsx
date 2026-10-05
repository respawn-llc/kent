import { render } from "@testing-library/react";
import type { DndContextProps, DragEndEvent } from "@dnd-kit/core";
import type * as DndKit from "@dnd-kit/core";
import { afterEach, expect, it, vi } from "vitest";
import { DragDropSurface } from "./DragDropSurface";

const context = vi.hoisted((): { props: DndContextProps | null } => ({ props: null }));

vi.mock("@dnd-kit/core", async (importOriginal) => ({
  ...(await importOriginal<typeof DndKit>()),
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
  const drop = () => {
    if (!context.props?.onDragEnd) throw new Error("Drag context did not mount.");
    context.props.onDragEnd(event);
  };
  if (mode === "development") expect(drop).toThrow(Error);
  else expect(drop).not.toThrow();
  expect(onCancel).toHaveBeenCalledOnce();
  expect(onCancel.mock.calls[0]?.[0]).toBeInstanceOf(Error);
  expect(onDrop).not.toHaveBeenCalled();
});
