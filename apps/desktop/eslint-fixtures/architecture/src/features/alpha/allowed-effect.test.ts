import { it, assert } from "@effect/vitest";
import { Effect } from "effect";

it.effect("permits standard owned test execution", () =>
  Effect.gen(function* () {
    yield* Effect.forkChild(Effect.void);
    assert.strictEqual(yield* Effect.succeed(1), 1);
  }),
);
