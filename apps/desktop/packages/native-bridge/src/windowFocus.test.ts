import * as Effect from "effect/Effect";
import * as Fiber from "effect/Fiber";
import * as Stream from "effect/Stream";
import { expect, vi } from "vitest";
import { it } from "@effect/vitest";
import { createTauriWindowFocusControls } from "./windowFocus";

const native = vi.hoisted(() => ({ onFocusChanged: vi.fn() }));
vi.mock("@tauri-apps/api/window", () => ({ getCurrentWindow: () => native }));

it.effect("releases a native registration that completes after its observer leaves", () =>
  Effect.gen(function* () {
    const resolve = vi.fn<(unlisten: () => void) => void>();
    const registration = new Promise<() => void>((complete) => {
      resolve.mockImplementation(complete);
    });
    const release = vi.fn();
    native.onFocusChanged.mockReturnValue(registration);
    const fiber = yield* Stream.runDrain(
      createTauriWindowFocusControls().focusChanges(async () => undefined),
    ).pipe(Effect.forkChild);
    yield* Effect.yieldNow;
    yield* Fiber.interrupt(fiber).pipe(Effect.forkChild);
    resolve(release);
    yield* Fiber.await(fiber);
    expect(native.onFocusChanged).toHaveBeenCalledTimes(1);
    expect(release).toHaveBeenCalledTimes(1);
  }),
);
