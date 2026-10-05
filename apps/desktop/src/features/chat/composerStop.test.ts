import { advanceComposerStop } from "./composerStop";

it("stops only on the second unhandled Escape inside two seconds", () => {
  const first = advanceComposerStop(null, { kind: "escape", now: 100, handled: false, stoppable: true });
  expect(first.stop).toBe(false);
  expect(
    advanceComposerStop(first.deadline, { kind: "escape", now: 2000, handled: false, stoppable: true }),
  ).toEqual({ deadline: null, stop: true });
});

it.each(["keyboard", "pointer", "focus", "disposal", "disconnected", "completed", "timeout"] as const)(
  "clears the arm on %s",
  (kind) => {
    expect(advanceComposerStop(2100, { kind })).toEqual({ deadline: null, stop: false });
  },
);

it("does not stop after timeout or arm without stoppable work", () => {
  expect(advanceComposerStop(2100, { kind: "escape", now: 2101, handled: false, stoppable: true }).stop).toBe(
    false,
  );
  expect(advanceComposerStop(null, { kind: "escape", now: 100, handled: false, stoppable: false })).toEqual({
    deadline: null,
    stop: false,
  });
});

it("lets handled Escape act only on its temporary surface", () => {
  expect(advanceComposerStop(2100, { kind: "escape", now: 200, handled: true, stoppable: true }).stop).toBe(
    false,
  );
  expect(
    advanceComposerStop(null, { kind: "escape", now: 200, handled: true, stoppable: true }).deadline,
  ).toBeNull();
});
